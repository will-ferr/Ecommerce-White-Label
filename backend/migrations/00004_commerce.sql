-- +goose Up
CREATE SCHEMA catalog;
CREATE SCHEMA inventory;
CREATE SCHEMA shipping;
CREATE SCHEMA cart;
CREATE SCHEMA orders;
CREATE SCHEMA payments;
CREATE SCHEMA checkout;
CREATE SCHEMA customers;

GRANT USAGE ON SCHEMA catalog, inventory, shipping, cart, orders, payments, checkout, customers TO app_runtime;

-- Catalog. Prices live on the variant until price lists and rules justify a module.
CREATE TABLE catalog.categories (
  tenant_id   uuid NOT NULL REFERENCES core.tenants (id),
  id          uuid NOT NULL,
  parent_id   uuid,
  name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  slug        text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 80),
  description text NOT NULL DEFAULT '' CHECK (length(description) <= 1000),
  position    integer NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, slug),
  FOREIGN KEY (tenant_id, parent_id) REFERENCES catalog.categories (tenant_id, id)
);

CREATE TABLE catalog.products (
  tenant_id       uuid NOT NULL REFERENCES core.tenants (id),
  id              uuid NOT NULL,
  category_id     uuid,
  name            text NOT NULL CHECK (length(name) BETWEEN 1 AND 160),
  slug            text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 120),
  description     text NOT NULL DEFAULT '' CHECK (length(description) <= 5000),
  brand           text NOT NULL DEFAULT '' CHECK (length(brand) <= 80),
  status          text NOT NULL CHECK (status IN ('draft', 'active', 'archived')),
  featured        boolean NOT NULL DEFAULT false,
  seo_title       text NOT NULL DEFAULT '' CHECK (length(seo_title) <= 70),
  seo_description text NOT NULL DEFAULT '' CHECK (length(seo_description) <= 160),
  version         integer NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, slug),
  FOREIGN KEY (tenant_id, category_id) REFERENCES catalog.categories (tenant_id, id)
);

CREATE INDEX products_listing_idx ON catalog.products (tenant_id, status, created_at DESC, id DESC);
CREATE INDEX products_category_idx ON catalog.products (tenant_id, category_id, status);

CREATE TABLE catalog.product_images (
  tenant_id  uuid NOT NULL,
  id         uuid NOT NULL,
  product_id uuid NOT NULL,
  url        text NOT NULL CHECK (url LIKE 'https://%' AND length(url) <= 2048),
  alt        text NOT NULL DEFAULT '' CHECK (length(alt) <= 160),
  position   integer NOT NULL,
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, product_id) REFERENCES catalog.products (tenant_id, id)
);

CREATE INDEX product_images_product_idx ON catalog.product_images (tenant_id, product_id, position);

CREATE TABLE catalog.variants (
  tenant_id         uuid NOT NULL,
  id                uuid NOT NULL,
  product_id        uuid NOT NULL,
  sku               text NOT NULL CHECK (sku ~ '^[A-Za-z0-9._-]{1,64}$'),
  gtin              text CHECK (gtin ~ '^[0-9]{8,14}$'),
  options           jsonb NOT NULL DEFAULT '{}',
  weight_grams      integer NOT NULL DEFAULT 0 CHECK (weight_grams >= 0),
  price_amount      bigint NOT NULL CHECK (price_amount >= 0),
  compare_at_amount bigint CHECK (compare_at_amount >= 0),
  cost_amount       bigint CHECK (cost_amount >= 0),
  position          integer NOT NULL DEFAULT 0,
  status            text NOT NULL CHECK (status IN ('active', 'archived')),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, sku),
  FOREIGN KEY (tenant_id, product_id) REFERENCES catalog.products (tenant_id, id)
);

CREATE INDEX variants_product_idx ON catalog.variants (tenant_id, product_id, position);

CREATE TABLE catalog.price_history (
  tenant_id         uuid NOT NULL,
  id                uuid NOT NULL,
  variant_id        uuid NOT NULL,
  price_amount      bigint NOT NULL,
  compare_at_amount bigint,
  cost_amount       bigint,
  changed_by        text NOT NULL,
  changed_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, variant_id) REFERENCES catalog.variants (tenant_id, id)
);

-- Inventory.
CREATE TABLE inventory.warehouses (
  tenant_id  uuid NOT NULL REFERENCES core.tenants (id),
  id         uuid NOT NULL,
  name       text NOT NULL,
  is_default boolean NOT NULL DEFAULT false,
  PRIMARY KEY (tenant_id, id)
);

CREATE UNIQUE INDEX warehouses_default_idx ON inventory.warehouses (tenant_id) WHERE is_default;

CREATE TABLE inventory.stock_levels (
  tenant_id    uuid NOT NULL,
  variant_id   uuid NOT NULL,
  warehouse_id uuid NOT NULL,
  on_hand      integer NOT NULL DEFAULT 0 CHECK (on_hand >= 0),
  reserved     integer NOT NULL DEFAULT 0 CHECK (reserved >= 0),
  min_quantity integer NOT NULL DEFAULT 0 CHECK (min_quantity >= 0),
  PRIMARY KEY (tenant_id, variant_id, warehouse_id),
  CHECK (reserved <= on_hand),
  FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id)
);

CREATE TABLE inventory.reservations (
  tenant_id    uuid NOT NULL,
  id           uuid NOT NULL,
  order_id     uuid NOT NULL,
  variant_id   uuid NOT NULL,
  warehouse_id uuid NOT NULL,
  quantity     integer NOT NULL CHECK (quantity > 0),
  status       text NOT NULL CHECK (status IN ('active', 'committed', 'released')),
  expires_at   timestamptz NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id)
);

CREATE INDEX reservations_order_idx ON inventory.reservations (tenant_id, order_id);

CREATE TABLE inventory.movements (
  tenant_id     uuid NOT NULL,
  id            uuid NOT NULL,
  variant_id    uuid NOT NULL,
  warehouse_id  uuid NOT NULL,
  kind          text NOT NULL CHECK (kind IN ('adjustment', 'sale', 'return')),
  quantity      integer NOT NULL,
  on_hand_after integer NOT NULL,
  reference     text NOT NULL,
  actor         text NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id)
);

CREATE INDEX movements_variant_idx ON inventory.movements (tenant_id, variant_id, created_at DESC);

-- Shipping methods configured by each store.
CREATE TABLE shipping.methods (
  tenant_id     uuid NOT NULL REFERENCES core.tenants (id),
  id            uuid NOT NULL,
  name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  kind          text NOT NULL CHECK (kind IN ('pickup', 'flat')),
  price_amount  bigint NOT NULL CHECK (price_amount >= 0),
  min_days      integer NOT NULL CHECK (min_days >= 0),
  max_days      integer NOT NULL CHECK (max_days >= min_days),
  active        boolean NOT NULL DEFAULT true,
  PRIMARY KEY (tenant_id, id)
);

-- Carts.
CREATE TABLE cart.carts (
  tenant_id  uuid NOT NULL REFERENCES core.tenants (id),
  id         uuid NOT NULL,
  token_hash bytea,
  user_id    uuid,
  status     text NOT NULL CHECK (status IN ('active', 'converted', 'merged')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, token_hash)
);

CREATE UNIQUE INDEX carts_active_user_idx ON cart.carts (tenant_id, user_id) WHERE status = 'active' AND user_id IS NOT NULL;

CREATE TABLE cart.items (
  tenant_id  uuid NOT NULL,
  cart_id    uuid NOT NULL,
  variant_id uuid NOT NULL,
  quantity   integer NOT NULL CHECK (quantity BETWEEN 1 AND 99),
  added_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, cart_id, variant_id),
  FOREIGN KEY (tenant_id, cart_id) REFERENCES cart.carts (tenant_id, id)
);

-- Orders.
CREATE TABLE orders.counters (
  tenant_id   uuid PRIMARY KEY REFERENCES core.tenants (id),
  next_number bigint NOT NULL
);

CREATE TABLE orders.orders (
  tenant_id         uuid NOT NULL REFERENCES core.tenants (id),
  id                uuid NOT NULL,
  number            bigint NOT NULL,
  status            text NOT NULL CHECK (status IN (
    'pending_payment', 'paid', 'processing', 'shipped', 'delivery_failed', 'delivered', 'completed', 'canceled'
  )),
  customer_user_id  uuid,
  customer_name     text NOT NULL,
  customer_email    text NOT NULL,
  shipping_address  jsonb NOT NULL,
  shipping_method   text NOT NULL,
  shipping_amount   bigint NOT NULL CHECK (shipping_amount >= 0),
  subtotal_amount   bigint NOT NULL CHECK (subtotal_amount >= 0),
  discount_amount   bigint NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
  tax_amount        bigint NOT NULL DEFAULT 0 CHECK (tax_amount >= 0),
  total_amount      bigint NOT NULL CHECK (total_amount >= 0),
  currency          text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  tracking_code     text NOT NULL DEFAULT '',
  cancel_reason     text NOT NULL DEFAULT '',
  payment_due_at    timestamptz NOT NULL,
  version           integer NOT NULL DEFAULT 1,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, number),
  CHECK (total_amount = subtotal_amount - discount_amount + shipping_amount + tax_amount)
);

CREATE INDEX orders_listing_idx ON orders.orders (tenant_id, status, created_at DESC, id DESC);
CREATE INDEX orders_customer_idx ON orders.orders (tenant_id, customer_user_id, created_at DESC);
CREATE INDEX orders_payment_due_idx ON orders.orders (payment_due_at) WHERE status = 'pending_payment';

CREATE TABLE orders.items (
  tenant_id     uuid NOT NULL,
  id            uuid NOT NULL,
  order_id      uuid NOT NULL,
  variant_id    uuid NOT NULL,
  product_name  text NOT NULL,
  variant_label text NOT NULL,
  sku           text NOT NULL,
  quantity      integer NOT NULL CHECK (quantity > 0),
  unit_amount   bigint NOT NULL CHECK (unit_amount >= 0),
  total_amount  bigint NOT NULL CHECK (total_amount = unit_amount * quantity),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, order_id) REFERENCES orders.orders (tenant_id, id)
);

CREATE INDEX order_items_order_idx ON orders.items (tenant_id, order_id);

CREATE TABLE orders.status_history (
  tenant_id   uuid NOT NULL,
  id          uuid NOT NULL,
  order_id    uuid NOT NULL,
  from_status text NOT NULL,
  to_status   text NOT NULL,
  actor_type  text NOT NULL CHECK (actor_type IN ('customer', 'user', 'system')),
  actor_id    text NOT NULL,
  note        text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, order_id) REFERENCES orders.orders (tenant_id, id)
);

CREATE INDEX order_history_order_idx ON orders.status_history (tenant_id, order_id, created_at);

CREATE TABLE orders.notes (
  tenant_id  uuid NOT NULL,
  id         uuid NOT NULL,
  order_id   uuid NOT NULL,
  author_id  uuid NOT NULL,
  body       text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, order_id) REFERENCES orders.orders (tenant_id, id)
);

-- Payments.
CREATE TABLE payments.payments (
  tenant_id          uuid NOT NULL REFERENCES core.tenants (id),
  id                 uuid NOT NULL,
  order_id           uuid NOT NULL,
  provider           text NOT NULL,
  method             text NOT NULL,
  status             text NOT NULL CHECK (status IN ('pending', 'captured', 'failed', 'canceled', 'refunded')),
  amount             bigint NOT NULL CHECK (amount >= 0),
  currency           text NOT NULL,
  provider_reference text NOT NULL DEFAULT '',
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, order_id)
);

CREATE TABLE payments.transactions (
  tenant_id          uuid NOT NULL,
  id                 uuid NOT NULL,
  payment_id         uuid NOT NULL,
  kind               text NOT NULL CHECK (kind IN ('charge', 'capture', 'failure', 'cancel', 'refund')),
  amount             bigint NOT NULL,
  provider_reference text NOT NULL DEFAULT '',
  created_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, payment_id) REFERENCES payments.payments (tenant_id, id)
);

-- Repeated checkout requests return the first answer instead of a second order.
CREATE TABLE checkout.idempotency_keys (
  tenant_id    uuid NOT NULL REFERENCES core.tenants (id),
  scope        text NOT NULL,
  key          text NOT NULL CHECK (key ~ '^[A-Za-z0-9_-]{16,100}$'),
  request_hash bytea NOT NULL,
  response     jsonb,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, scope, key)
);

-- Customer address book.
CREATE TABLE customers.addresses (
  tenant_id   uuid NOT NULL REFERENCES core.tenants (id),
  id          uuid NOT NULL,
  user_id     uuid NOT NULL,
  label       text NOT NULL DEFAULT '' CHECK (length(label) <= 40),
  recipient   text NOT NULL,
  postal_code text NOT NULL CHECK (postal_code ~ '^[0-9]{8}$'),
  street      text NOT NULL,
  number      text NOT NULL,
  complement  text NOT NULL DEFAULT '',
  district    text NOT NULL,
  city        text NOT NULL,
  state       text NOT NULL CHECK (state ~ '^[A-Z]{2}$'),
  is_default  boolean NOT NULL DEFAULT false,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);

CREATE INDEX addresses_user_idx ON customers.addresses (tenant_id, user_id);

-- +goose StatementBegin
DO $$
DECLARE
  table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'catalog.categories', 'catalog.products', 'catalog.product_images', 'catalog.variants', 'catalog.price_history',
    'inventory.warehouses', 'inventory.stock_levels', 'inventory.reservations', 'inventory.movements',
    'shipping.methods', 'cart.carts', 'cart.items',
    'orders.counters', 'orders.orders', 'orders.items', 'orders.status_history', 'orders.notes',
    'payments.payments', 'payments.transactions', 'checkout.idempotency_keys', 'customers.addresses'
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

GRANT SELECT, INSERT, UPDATE, DELETE ON catalog.categories, catalog.product_images, cart.items, customers.addresses TO app_runtime;
GRANT SELECT, INSERT, UPDATE ON catalog.products, catalog.variants, inventory.warehouses, inventory.stock_levels,
  inventory.reservations, shipping.methods, cart.carts, orders.counters, orders.orders, payments.payments,
  checkout.idempotency_keys TO app_runtime;
GRANT SELECT, INSERT ON catalog.price_history, inventory.movements, orders.items, orders.status_history, orders.notes,
  payments.transactions TO app_runtime;

-- The expiry job lists overdue orders of every store, then handles each one
-- inside the context of its own store. This function reveals only identifiers.
CREATE POLICY overdue_orders ON orders.orders FOR SELECT TO app_owner USING (status = 'pending_payment');

-- +goose StatementBegin
CREATE FUNCTION orders.overdue_orders(p_limit integer)
RETURNS TABLE (tenant_id uuid, order_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, orders
AS $$
  SELECT o.tenant_id, o.id
  FROM orders.orders o
  WHERE o.status = 'pending_payment' AND o.payment_due_at < now()
  ORDER BY o.payment_due_at
  LIMIT least(greatest(p_limit, 1), 500)
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION orders.overdue_orders(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION orders.overdue_orders(integer) TO app_runtime;

INSERT INTO identity.permissions (code, description) VALUES
  ('catalog:read', 'Consultar produtos e categorias'),
  ('catalog:manage', 'Gerenciar produtos, categorias e preços'),
  ('inventory:manage', 'Ajustar estoque'),
  ('orders:read', 'Consultar pedidos'),
  ('orders:manage', 'Atualizar e cancelar pedidos'),
  ('shipping:manage', 'Gerenciar formas de entrega');

INSERT INTO identity.role_permissions (role_code, permission_code) VALUES
  ('owner', 'catalog:read'), ('owner', 'catalog:manage'), ('owner', 'inventory:manage'),
  ('owner', 'orders:read'), ('owner', 'orders:manage'), ('owner', 'shipping:manage'),
  ('manager', 'catalog:read'), ('manager', 'catalog:manage'), ('manager', 'inventory:manage'),
  ('manager', 'orders:read'), ('manager', 'orders:manage'), ('manager', 'shipping:manage'),
  ('support', 'catalog:read'), ('support', 'orders:read'), ('support', 'orders:manage'),
  ('viewer', 'catalog:read'), ('viewer', 'orders:read');

-- +goose Down
DELETE FROM identity.role_permissions WHERE permission_code IN
  ('catalog:read', 'catalog:manage', 'inventory:manage', 'orders:read', 'orders:manage', 'shipping:manage');
DELETE FROM identity.permissions WHERE code IN
  ('catalog:read', 'catalog:manage', 'inventory:manage', 'orders:read', 'orders:manage', 'shipping:manage');
DROP SCHEMA customers CASCADE;
DROP SCHEMA checkout CASCADE;
DROP SCHEMA payments CASCADE;
DROP SCHEMA orders CASCADE;
DROP SCHEMA cart CASCADE;
DROP SCHEMA shipping CASCADE;
DROP SCHEMA inventory CASCADE;
DROP SCHEMA catalog CASCADE;
