-- +goose Up

-- Extensions live in their own schema, so the application role only reaches
-- the functions wrapped below. Both extensions are trusted in PostgreSQL.
CREATE SCHEMA ext;
CREATE EXTENSION IF NOT EXISTS unaccent SCHEMA ext;
CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA ext;

-- Product search (ADR-011): accent-insensitive Portuguese full text search,
-- with trigram similarity on the name to tolerate typos.
-- +goose StatementBegin
CREATE FUNCTION catalog.search_text(value text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
SET search_path = pg_catalog
RETURN ext.unaccent('ext.unaccent'::regdictionary, lower(value));
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION catalog.search_query(query text) RETURNS tsquery
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
SET search_path = pg_catalog
RETURN websearch_to_tsquery('portuguese'::regconfig, catalog.search_text(query));
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION catalog.name_similarity(query text, name text) RETURNS real
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
SET search_path = pg_catalog
RETURN ext.word_similarity(catalog.search_text(query), catalog.search_text(name));
-- +goose StatementEnd

-- The searchable document of a product. An expression index keeps it out of
-- the table rows, so listings do not carry it.
-- +goose StatementBegin
CREATE FUNCTION catalog.product_document(name text, brand text, description text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
SET search_path = pg_catalog
RETURN setweight(to_tsvector('portuguese'::regconfig, catalog.search_text(name)), 'A') ||
       setweight(to_tsvector('portuguese'::regconfig, catalog.search_text(brand)), 'B') ||
       setweight(to_tsvector('portuguese'::regconfig, catalog.search_text(description)), 'C');
-- +goose StatementEnd

CREATE INDEX products_search_idx ON catalog.products USING gin (catalog.product_document(name, brand, description));

GRANT USAGE ON SCHEMA ext TO app_runtime;
GRANT EXECUTE ON FUNCTION catalog.search_text(text), catalog.search_query(text), catalog.name_similarity(text, text),
  catalog.product_document(text, text, text) TO app_runtime;

-- Orders remember the address of the store where they were placed, so
-- messages sent later link back to it.
ALTER TABLE orders.orders ADD COLUMN store_host text NOT NULL DEFAULT '' CHECK (length(store_host) <= 253);

-- Transactional outbox (ADR-009): events are written with the business change
-- and published to RabbitMQ by the relay.
CREATE SCHEMA messaging;
GRANT USAGE ON SCHEMA messaging TO app_runtime;

CREATE TABLE messaging.outbox (
  tenant_id    uuid NOT NULL REFERENCES core.tenants (id),
  id           uuid NOT NULL,
  type         text NOT NULL CHECK (type ~ '^[a-z_]+(\.[a-z_]+)+\.v[0-9]+$'),
  source       text NOT NULL CHECK (length(source) <= 100),
  aggregate_id uuid NOT NULL,
  payload      jsonb NOT NULL,
  traceparent  text NOT NULL DEFAULT '' CHECK (length(traceparent) <= 100),
  created_at   timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz,
  PRIMARY KEY (tenant_id, id)
);

CREATE INDEX outbox_pending_idx ON messaging.outbox (created_at) WHERE published_at IS NULL;
CREATE INDEX outbox_published_idx ON messaging.outbox (published_at) WHERE published_at IS NOT NULL;

-- Events already handled by each consumer, written in the same transaction as
-- the effect of the event.
CREATE TABLE messaging.inbox (
  tenant_id    uuid NOT NULL REFERENCES core.tenants (id),
  consumer     text NOT NULL CHECK (consumer ~ '^[a-z][a-z0-9-]{1,40}$'),
  event_id     uuid NOT NULL,
  processed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, consumer, event_id)
);

CREATE INDEX inbox_processed_idx ON messaging.inbox (processed_at);

-- Durable e-mail. The message is sealed with AES-GCM, because it may carry
-- sign-in links, and erased as soon as it is delivered.
CREATE SCHEMA notifications;
GRANT USAGE ON SCHEMA notifications TO app_runtime;

CREATE TABLE notifications.emails (
  tenant_id       uuid NOT NULL REFERENCES core.tenants (id),
  id              uuid NOT NULL,
  kind            text NOT NULL CHECK (kind ~ '^[a-z_.]{3,60}$'),
  sealed          bytea,
  status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error      text NOT NULL DEFAULT '' CHECK (length(last_error) <= 500),
  created_at      timestamptz NOT NULL DEFAULT now(),
  sent_at         timestamptz,
  PRIMARY KEY (tenant_id, id),
  CHECK (status = 'pending' OR sealed IS NULL)
);

CREATE INDEX emails_due_idx ON notifications.emails (next_attempt_at) WHERE status = 'pending';

-- Payment notifications received from gateways. The unique key makes a
-- repeated delivery of the same notification a no-op.
CREATE TABLE payments.webhook_events (
  tenant_id          uuid NOT NULL REFERENCES core.tenants (id),
  id                 uuid NOT NULL,
  provider           text NOT NULL,
  provider_event_id  text NOT NULL CHECK (length(provider_event_id) BETWEEN 1 AND 200),
  provider_reference text NOT NULL CHECK (length(provider_reference) <= 200),
  status             text NOT NULL CHECK (length(status) <= 40),
  amount             bigint,
  outcome            text NOT NULL DEFAULT '' CHECK (length(outcome) <= 60),
  received_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, provider, provider_event_id)
);

ALTER TABLE payments.payments DROP CONSTRAINT payments_status_check;
ALTER TABLE payments.payments ADD CONSTRAINT payments_status_check
  CHECK (status IN ('pending', 'captured', 'failed', 'canceled', 'refund_pending', 'refunded'));
CREATE INDEX payments_reference_idx ON payments.payments (tenant_id, provider, provider_reference);

-- Shipping rates by postal code range and weight.
ALTER TABLE shipping.methods DROP CONSTRAINT methods_kind_check;
ALTER TABLE shipping.methods ADD CONSTRAINT methods_kind_check CHECK (kind IN ('pickup', 'flat', 'table'));

CREATE TABLE shipping.rates (
  tenant_id         uuid NOT NULL,
  id                uuid NOT NULL,
  method_id         uuid NOT NULL,
  postal_code_start text NOT NULL CHECK (postal_code_start ~ '^[0-9]{8}$'),
  postal_code_end   text NOT NULL CHECK (postal_code_end ~ '^[0-9]{8}$'),
  max_weight_grams  integer NOT NULL CHECK (max_weight_grams > 0),
  price_amount      bigint NOT NULL CHECK (price_amount >= 0),
  min_days          integer NOT NULL CHECK (min_days >= 0),
  max_days          integer NOT NULL CHECK (max_days >= min_days),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, method_id) REFERENCES shipping.methods (tenant_id, id) ON DELETE CASCADE,
  CHECK (postal_code_start <= postal_code_end)
);

CREATE INDEX rates_method_idx ON shipping.rates (tenant_id, method_id, postal_code_start);

-- +goose StatementBegin
DO $$
DECLARE
  table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'messaging.outbox', 'messaging.inbox', 'notifications.emails', 'payments.webhook_events', 'shipping.rates'
  ] LOOP
    EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', table_name);
    EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', table_name);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %s TO app_runtime
         USING (tenant_id = (SELECT app.current_tenant_id()))
         WITH CHECK (tenant_id = (SELECT app.current_tenant_id()))',
      table_name
    );
  END LOOP;
END
$$;
-- +goose StatementEnd

GRANT INSERT ON messaging.outbox TO app_runtime;
GRANT SELECT, INSERT ON messaging.inbox, payments.webhook_events TO app_runtime;
GRANT UPDATE (outcome) ON payments.webhook_events TO app_runtime;
GRANT INSERT ON notifications.emails TO app_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON shipping.rates TO app_runtime;

-- Background work that crosses stores runs through the functions below. They
-- belong to the migration role, which alone has these policies, and they
-- expose only what each job needs.
CREATE POLICY relay ON messaging.outbox TO app_owner USING (true) WITH CHECK (true);
CREATE POLICY retention ON messaging.inbox TO app_owner USING (true);
CREATE POLICY dispatcher ON notifications.emails TO app_owner USING (true) WITH CHECK (true);

-- Locks a batch of unpublished events. The locks last until the caller's
-- transaction ends, so concurrent relays never publish the same rows.
-- +goose StatementBegin
CREATE FUNCTION messaging.claim_outbox(p_limit integer)
RETURNS TABLE (tenant_id uuid, id uuid, type text, source text, aggregate_id uuid, payload jsonb, traceparent text, created_at timestamptz)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, messaging
AS $$
  SELECT o.tenant_id, o.id, o.type, o.source, o.aggregate_id, o.payload, o.traceparent, o.created_at
  FROM messaging.outbox o
  WHERE o.published_at IS NULL
  ORDER BY o.created_at
  LIMIT least(greatest(p_limit, 1), 500)
  FOR UPDATE SKIP LOCKED
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION messaging.mark_published(p_ids uuid[])
RETURNS void
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, messaging
AS $$
  UPDATE messaging.outbox SET published_at = now() WHERE id = ANY (p_ids) AND published_at IS NULL
$$;
-- +goose StatementEnd

-- Age of the oldest unpublished event, in seconds, for the relay lag metric.
-- +goose StatementBegin
CREATE FUNCTION messaging.outbox_lag()
RETURNS double precision
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, messaging
AS $$
  SELECT coalesce(extract(epoch FROM now() - min(created_at)), 0)::double precision
  FROM messaging.outbox WHERE published_at IS NULL
$$;
-- +goose StatementEnd

-- Removes published events and inbox entries older than the retention.
-- +goose StatementBegin
CREATE FUNCTION messaging.purge(p_older_than interval)
RETURNS bigint
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, messaging
AS $$
DECLARE
  removed bigint;
  more bigint;
BEGIN
  IF p_older_than < interval '1 day' THEN
    RAISE EXCEPTION 'retention must be at least one day';
  END IF;
  DELETE FROM messaging.outbox WHERE published_at < now() - p_older_than;
  GET DIAGNOSTICS removed = ROW_COUNT;
  DELETE FROM messaging.inbox WHERE processed_at < now() - p_older_than;
  GET DIAGNOSTICS more = ROW_COUNT;
  RETURN removed + more;
END
$$;
-- +goose StatementEnd

-- Leases due e-mails to one dispatcher. The lease is the next attempt time, so
-- a crashed dispatcher only delays the message.
-- +goose StatementBegin
CREATE FUNCTION notifications.claim_emails(p_limit integer, p_lease interval)
RETURNS TABLE (tenant_id uuid, id uuid, kind text, sealed bytea, attempts integer)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, notifications
AS $$
  UPDATE notifications.emails e
     SET next_attempt_at = now() + p_lease, attempts = e.attempts + 1
   WHERE (e.tenant_id, e.id) IN (
     SELECT d.tenant_id, d.id FROM notifications.emails d
      WHERE d.status = 'pending' AND d.next_attempt_at <= now()
      ORDER BY d.next_attempt_at
      LIMIT least(greatest(p_limit, 1), 100)
      FOR UPDATE SKIP LOCKED)
  RETURNING e.tenant_id, e.id, e.kind, e.sealed, e.attempts
$$;
-- +goose StatementEnd

-- Records the result of a delivery attempt. A failure with p_retry_in NULL is final.
-- +goose StatementBegin
CREATE FUNCTION notifications.finish_email(p_tenant_id uuid, p_id uuid, p_error text, p_retry_in interval)
RETURNS void
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, notifications
AS $$
  UPDATE notifications.emails
     SET status = CASE WHEN p_error IS NULL THEN 'sent' WHEN p_retry_in IS NULL THEN 'failed' ELSE 'pending' END,
         sealed = CASE WHEN p_error IS NULL OR p_retry_in IS NULL THEN NULL ELSE sealed END,
         sent_at = CASE WHEN p_error IS NULL THEN now() END,
         last_error = left(coalesce(p_error, ''), 500),
         next_attempt_at = now() + coalesce(p_retry_in, interval '0')
   WHERE tenant_id = p_tenant_id AND id = p_id AND status = 'pending'
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION messaging.claim_outbox(integer), messaging.mark_published(uuid[]), messaging.outbox_lag(),
  messaging.purge(interval), notifications.claim_emails(integer, interval),
  notifications.finish_email(uuid, uuid, text, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION messaging.claim_outbox(integer), messaging.mark_published(uuid[]), messaging.outbox_lag(),
  messaging.purge(interval), notifications.claim_emails(integer, interval),
  notifications.finish_email(uuid, uuid, text, interval) TO app_runtime;

-- +goose Down
DROP SCHEMA notifications CASCADE;
DROP SCHEMA messaging CASCADE;
DROP TABLE shipping.rates;
DELETE FROM shipping.methods WHERE kind = 'table';
ALTER TABLE shipping.methods DROP CONSTRAINT methods_kind_check;
ALTER TABLE shipping.methods ADD CONSTRAINT methods_kind_check CHECK (kind IN ('pickup', 'flat'));
DROP INDEX payments.payments_reference_idx;
UPDATE payments.payments SET status = 'captured' WHERE status = 'refund_pending';
ALTER TABLE payments.payments DROP CONSTRAINT payments_status_check;
ALTER TABLE payments.payments ADD CONSTRAINT payments_status_check
  CHECK (status IN ('pending', 'captured', 'failed', 'canceled', 'refunded'));
DROP TABLE payments.webhook_events;
ALTER TABLE orders.orders DROP COLUMN store_host;
DROP INDEX catalog.products_search_idx;
DROP FUNCTION catalog.product_document(text, text, text);
DROP FUNCTION catalog.name_similarity(text, text);
DROP FUNCTION catalog.search_query(text);
DROP FUNCTION catalog.search_text(text);
DROP SCHEMA ext CASCADE;
