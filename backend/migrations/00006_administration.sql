-- +goose Up

INSERT INTO identity.permissions (code, description) VALUES
  ('stores:read', 'Consultar as lojas da plataforma'),
  ('stores:manage', 'Criar lojas, alterar status, domínios e recursos'),
  ('store:manage', 'Alterar dados, SEO e tema da loja'),
  ('promotions:manage', 'Gerenciar cupons'),
  ('content:manage', 'Gerenciar páginas de conteúdo'),
  ('reports:read', 'Consultar relatórios de vendas'),
  ('customers:read', 'Consultar clientes');

INSERT INTO identity.role_permissions (role_code, permission_code) VALUES
  ('platform_admin', 'stores:read'), ('platform_admin', 'stores:manage'),
  ('platform_support', 'stores:read'),
  ('owner', 'store:manage'), ('owner', 'promotions:manage'), ('owner', 'content:manage'),
  ('owner', 'reports:read'), ('owner', 'customers:read'),
  ('manager', 'store:manage'), ('manager', 'promotions:manage'), ('manager', 'content:manage'),
  ('manager', 'reports:read'), ('manager', 'customers:read'),
  ('support', 'customers:read'),
  ('viewer', 'reports:read');

-- Store teams change their own name, SEO and theme. Row Level Security keeps
-- each store to its own rows; plan, status and domains stay with the platform.
GRANT UPDATE (store_name, seo_title, seo_description, seo_indexable, updated_at) ON core.tenant_settings TO app_runtime;
GRANT INSERT, UPDATE (status) ON core.theme_versions TO app_runtime;

-- The console sends invitations by e-mail. It may only add messages.
GRANT USAGE ON SCHEMA notifications TO app_platform;
GRANT INSERT ON notifications.emails TO app_platform;
CREATE POLICY platform_access ON notifications.emails FOR INSERT TO app_platform WITH CHECK (true);

-- Coupons.
CREATE SCHEMA promotions;
GRANT USAGE ON SCHEMA promotions TO app_runtime;

CREATE TABLE promotions.coupons (
  tenant_id          uuid NOT NULL REFERENCES core.tenants (id),
  id                 uuid NOT NULL,
  code               text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{3,30}$'),
  description        text NOT NULL DEFAULT '' CHECK (length(description) <= 160),
  kind               text NOT NULL CHECK (kind IN ('percent', 'fixed', 'free_shipping')),
  value              bigint NOT NULL CHECK (value >= 0),
  min_subtotal       bigint NOT NULL DEFAULT 0 CHECK (min_subtotal >= 0),
  starts_at          timestamptz,
  ends_at            timestamptz,
  usage_limit        integer CHECK (usage_limit > 0),
  per_customer_limit integer CHECK (per_customer_limit > 0),
  used_count         integer NOT NULL DEFAULT 0 CHECK (used_count >= 0),
  active             boolean NOT NULL DEFAULT true,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, code),
  CHECK (usage_limit IS NULL OR used_count <= usage_limit),
  CHECK (kind <> 'percent' OR value BETWEEN 1 AND 100),
  CHECK (starts_at IS NULL OR ends_at IS NULL OR starts_at < ends_at)
);

CREATE TABLE promotions.redemptions (
  tenant_id       uuid NOT NULL,
  id              uuid NOT NULL,
  coupon_id       uuid NOT NULL,
  order_id        uuid NOT NULL,
  customer_email  text NOT NULL,
  discount_amount bigint NOT NULL CHECK (discount_amount >= 0),
  status          text NOT NULL CHECK (status IN ('applied', 'released')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, order_id),
  FOREIGN KEY (tenant_id, coupon_id) REFERENCES promotions.coupons (tenant_id, id)
);

CREATE INDEX redemptions_customer_idx ON promotions.redemptions (tenant_id, coupon_id, customer_email) WHERE status = 'applied';

ALTER TABLE orders.orders ADD COLUMN coupon_code text NOT NULL DEFAULT '' CHECK (length(coupon_code) <= 30);

-- Content pages, such as "About" and the privacy policy.
CREATE SCHEMA content;
GRANT USAGE ON SCHEMA content TO app_runtime;

CREATE TABLE content.pages (
  tenant_id       uuid NOT NULL REFERENCES core.tenants (id),
  id              uuid NOT NULL,
  slug            text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 80),
  title           text NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
  body            text NOT NULL CHECK (length(body) <= 20000),
  seo_description text NOT NULL DEFAULT '' CHECK (length(seo_description) <= 160),
  status          text NOT NULL CHECK (status IN ('draft', 'published')),
  show_in_footer  boolean NOT NULL DEFAULT false,
  version         integer NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, slug)
);

-- Erased customer accounts keep their row, so orders stay consistent, but
-- lose every personal field.
ALTER TABLE identity.users DROP CONSTRAINT users_status_check;
ALTER TABLE identity.users ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'invited', 'disabled', 'erased'));

-- +goose StatementBegin
DO $$
DECLARE
  table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY['promotions.coupons', 'promotions.redemptions', 'content.pages'] LOOP
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

GRANT SELECT, INSERT, UPDATE ON promotions.coupons, promotions.redemptions TO app_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON content.pages TO app_runtime;

-- +goose Down
UPDATE identity.users SET status = 'disabled' WHERE status = 'erased';
ALTER TABLE identity.users DROP CONSTRAINT users_status_check;
ALTER TABLE identity.users ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'invited', 'disabled'));
DROP SCHEMA content CASCADE;
ALTER TABLE orders.orders DROP COLUMN coupon_code;
DROP SCHEMA promotions CASCADE;
DROP POLICY platform_access ON notifications.emails;
REVOKE INSERT ON notifications.emails FROM app_platform;
REVOKE USAGE ON SCHEMA notifications FROM app_platform;
REVOKE INSERT, UPDATE (status) ON core.theme_versions FROM app_runtime;
REVOKE UPDATE (store_name, seo_title, seo_description, seo_indexable, updated_at) ON core.tenant_settings FROM app_runtime;
DELETE FROM identity.role_permissions WHERE permission_code IN
  ('stores:read', 'stores:manage', 'store:manage', 'promotions:manage', 'content:manage', 'reports:read', 'customers:read');
DELETE FROM identity.permissions WHERE code IN
  ('stores:read', 'stores:manage', 'store:manage', 'promotions:manage', 'content:manage', 'reports:read', 'customers:read');
