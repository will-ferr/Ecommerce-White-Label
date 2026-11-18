-- +goose Up
CREATE SCHEMA core;

GRANT USAGE ON SCHEMA core TO app_runtime, app_platform;

-- Global catalogs, shared by every tenant.
CREATE TABLE core.plans (
  id         uuid PRIMARY KEY,
  code       text NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9-]{1,30}$'),
  name       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE core.feature_flags (
  key         text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]{1,40}$'),
  description text NOT NULL,
  enabled     boolean NOT NULL DEFAULT true
);

CREATE TABLE core.plan_features (
  plan_id     uuid NOT NULL REFERENCES core.plans (id),
  feature_key text NOT NULL REFERENCES core.feature_flags (key),
  PRIMARY KEY (plan_id, feature_key)
);

-- Tenant data.
CREATE TABLE core.tenants (
  id         uuid PRIMARY KEY,
  slug       text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$'),
  plan_id    uuid NOT NULL REFERENCES core.plans (id),
  status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tenants_plan_id_idx ON core.tenants (plan_id);

CREATE TABLE core.tenant_domains (
  host               text PRIMARY KEY CHECK (host = lower(host) AND length(host) <= 253),
  tenant_id          uuid NOT NULL REFERENCES core.tenants (id),
  kind               text NOT NULL CHECK (kind IN ('subdomain', 'custom')),
  verification_token text,
  verified_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tenant_domains_tenant_id_idx ON core.tenant_domains (tenant_id);

CREATE TABLE core.tenant_settings (
  tenant_id       uuid PRIMARY KEY REFERENCES core.tenants (id),
  store_name      text NOT NULL CHECK (length(store_name) BETWEEN 1 AND 120),
  currency        text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  locale          text NOT NULL,
  timezone        text NOT NULL,
  seo_title       text NOT NULL DEFAULT '' CHECK (length(seo_title) <= 70),
  seo_description text NOT NULL DEFAULT '' CHECK (length(seo_description) <= 160),
  seo_indexable   boolean NOT NULL DEFAULT false,
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE core.tenant_features (
  tenant_id   uuid NOT NULL REFERENCES core.tenants (id),
  feature_key text NOT NULL REFERENCES core.feature_flags (key),
  enabled     boolean NOT NULL,
  PRIMARY KEY (tenant_id, feature_key)
);

CREATE TABLE core.theme_versions (
  tenant_id  uuid NOT NULL REFERENCES core.tenants (id),
  id         uuid NOT NULL,
  version    integer NOT NULL CHECK (version > 0),
  status     text NOT NULL CHECK (status IN ('published', 'archived')),
  tokens     jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, version)
);

CREATE UNIQUE INDEX theme_versions_one_published_idx
  ON core.theme_versions (tenant_id)
  WHERE status = 'published';

-- Row Level Security. Tables without a policy for a role return no rows to it.
ALTER TABLE core.tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE core.tenant_domains ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.tenant_domains FORCE ROW LEVEL SECURITY;
ALTER TABLE core.tenant_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.tenant_settings FORCE ROW LEVEL SECURITY;
ALTER TABLE core.tenant_features ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.tenant_features FORCE ROW LEVEL SECURITY;
ALTER TABLE core.theme_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.theme_versions FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON core.tenants TO app_runtime
  USING (id = (SELECT app.current_tenant_id()))
  WITH CHECK (id = (SELECT app.current_tenant_id()));
CREATE POLICY tenant_isolation ON core.tenant_settings TO app_runtime
  USING (tenant_id = (SELECT app.current_tenant_id()))
  WITH CHECK (tenant_id = (SELECT app.current_tenant_id()));
CREATE POLICY tenant_isolation ON core.tenant_features TO app_runtime
  USING (tenant_id = (SELECT app.current_tenant_id()))
  WITH CHECK (tenant_id = (SELECT app.current_tenant_id()));
CREATE POLICY tenant_isolation ON core.theme_versions TO app_runtime
  USING (tenant_id = (SELECT app.current_tenant_id()))
  WITH CHECK (tenant_id = (SELECT app.current_tenant_id()));

CREATE POLICY platform_access ON core.tenants TO app_platform USING (true) WITH CHECK (true);
CREATE POLICY platform_access ON core.tenant_domains TO app_platform USING (true) WITH CHECK (true);
CREATE POLICY platform_access ON core.tenant_settings TO app_platform USING (true) WITH CHECK (true);
CREATE POLICY platform_access ON core.tenant_features TO app_platform USING (true) WITH CHECK (true);
CREATE POLICY platform_access ON core.theme_versions TO app_platform USING (true) WITH CHECK (true);

-- Only the host resolution function runs as the owner and reads across tenants.
CREATE POLICY host_resolution ON core.tenants FOR SELECT TO app_owner USING (true);
CREATE POLICY host_resolution ON core.tenant_domains FOR SELECT TO app_owner USING (true);

GRANT SELECT ON core.plans, core.feature_flags, core.plan_features TO app_runtime, app_platform;
GRANT SELECT ON core.tenants, core.tenant_settings, core.tenant_features, core.theme_versions TO app_runtime;
GRANT SELECT, INSERT, UPDATE ON core.tenants, core.tenant_settings, core.theme_versions TO app_platform;
GRANT SELECT, INSERT, UPDATE, DELETE ON core.tenant_domains, core.tenant_features TO app_platform;

-- +goose StatementBegin
-- Runs before a tenant context exists, so it returns only what routing needs.
CREATE FUNCTION core.resolve_tenant_by_host(p_host text)
RETURNS TABLE (tenant_id uuid, status text, plan_code text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, core
AS $$
  SELECT t.id, t.status, p.code
  FROM core.tenant_domains d
  JOIN core.tenants t ON t.id = d.tenant_id
  JOIN core.plans p ON p.id = t.plan_id
  WHERE d.host = lower(p_host)
    AND d.verified_at IS NOT NULL
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION core.resolve_tenant_by_host(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION core.resolve_tenant_by_host(text) TO app_runtime;

-- Initial catalogs.
INSERT INTO core.feature_flags (key, description) VALUES
  ('reviews', 'Avaliações de produtos'),
  ('wishlist', 'Lista de favoritos'),
  ('loyalty', 'Programa de fidelidade'),
  ('subscriptions', 'Produtos por assinatura'),
  ('marketplace', 'Múltiplos vendedores');

INSERT INTO core.plans (id, code, name) VALUES
  ('01960000-0000-7000-8000-000000000101', 'starter', 'Starter'),
  ('01960000-0000-7000-8000-000000000102', 'growth', 'Growth'),
  ('01960000-0000-7000-8000-000000000103', 'enterprise', 'Enterprise');

INSERT INTO core.plan_features (plan_id, feature_key) VALUES
  ('01960000-0000-7000-8000-000000000101', 'reviews'),
  ('01960000-0000-7000-8000-000000000101', 'wishlist'),
  ('01960000-0000-7000-8000-000000000102', 'reviews'),
  ('01960000-0000-7000-8000-000000000102', 'wishlist'),
  ('01960000-0000-7000-8000-000000000102', 'loyalty'),
  ('01960000-0000-7000-8000-000000000102', 'subscriptions'),
  ('01960000-0000-7000-8000-000000000103', 'reviews'),
  ('01960000-0000-7000-8000-000000000103', 'wishlist'),
  ('01960000-0000-7000-8000-000000000103', 'loyalty'),
  ('01960000-0000-7000-8000-000000000103', 'subscriptions'),
  ('01960000-0000-7000-8000-000000000103', 'marketplace');

-- +goose Down
DROP SCHEMA core CASCADE;
