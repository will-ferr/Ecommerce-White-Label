-- +goose Up
CREATE SCHEMA identity;
CREATE SCHEMA audit;

GRANT USAGE ON SCHEMA identity, audit TO app_runtime, app_platform;

-- Reserved tenant that holds platform operators. It has no domain, so no
-- storefront request can ever resolve to it.
CREATE POLICY migration_seed ON core.tenants FOR INSERT TO app_owner
  WITH CHECK (id = '01960000-0000-7000-8000-000000000001');
INSERT INTO core.tenants (id, slug, plan_id)
VALUES ('01960000-0000-7000-8000-000000000001', 'platform', '01960000-0000-7000-8000-000000000103');
DROP POLICY migration_seed ON core.tenants;

-- +goose StatementBegin
CREATE FUNCTION core.resolve_tenant_by_slug(p_slug text)
RETURNS TABLE (tenant_id uuid, status text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, core
AS $$
  SELECT t.id, t.status
  FROM core.tenants t
  WHERE t.slug = lower(p_slug)
    AND t.id <> '01960000-0000-7000-8000-000000000001'
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION core.tenant_is_active(p_tenant_id uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, core
AS $$
  SELECT EXISTS (SELECT 1 FROM core.tenants t WHERE t.id = p_tenant_id AND t.status = 'active')
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION core.resolve_tenant_by_slug(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION core.tenant_is_active(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION core.resolve_tenant_by_slug(text) TO app_runtime, app_platform;
GRANT EXECUTE ON FUNCTION core.tenant_is_active(uuid) TO app_runtime;

-- Global authorization catalogs.
CREATE TABLE identity.permissions (
  code        text PRIMARY KEY CHECK (code ~ '^[a-z_]+:[a-z_]+$'),
  description text NOT NULL
);

CREATE TABLE identity.roles (
  code  text PRIMARY KEY CHECK (code ~ '^[a-z_]+$'),
  realm text NOT NULL CHECK (realm IN ('staff', 'platform')),
  name  text NOT NULL,
  rank  integer NOT NULL CHECK (rank > 0)
);

CREATE TABLE identity.role_permissions (
  role_code       text NOT NULL REFERENCES identity.roles (code),
  permission_code text NOT NULL REFERENCES identity.permissions (code),
  PRIMARY KEY (role_code, permission_code)
);

INSERT INTO identity.permissions (code, description) VALUES
  ('team:read', 'Consultar a equipe'),
  ('team:manage', 'Convidar e gerenciar a equipe'),
  ('audit:read', 'Consultar a trilha de auditoria');

INSERT INTO identity.roles (code, realm, name, rank) VALUES
  ('owner', 'staff', 'Proprietário', 40),
  ('manager', 'staff', 'Gerente', 30),
  ('support', 'staff', 'Atendimento', 20),
  ('viewer', 'staff', 'Leitura', 10),
  ('platform_admin', 'platform', 'Administrador da plataforma', 40),
  ('platform_support', 'platform', 'Suporte da plataforma', 20);

INSERT INTO identity.role_permissions (role_code, permission_code) VALUES
  ('owner', 'team:read'),
  ('owner', 'team:manage'),
  ('owner', 'audit:read'),
  ('manager', 'team:read'),
  ('manager', 'audit:read'),
  ('support', 'team:read'),
  ('platform_admin', 'team:read'),
  ('platform_admin', 'team:manage'),
  ('platform_admin', 'audit:read'),
  ('platform_support', 'team:read'),
  ('platform_support', 'audit:read');

-- Accounts.
CREATE TABLE identity.users (
  tenant_id            uuid NOT NULL REFERENCES core.tenants (id),
  id                   uuid NOT NULL,
  realm                text NOT NULL CHECK (realm IN ('customer', 'staff', 'platform')),
  email                text NOT NULL CHECK (email = lower(email) AND length(email) <= 254),
  name                 text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  password_hash        text,
  status               text NOT NULL CHECK (status IN ('active', 'invited', 'disabled')),
  email_verified_at    timestamptz,
  mfa_secret_encrypted bytea,
  mfa_enabled_at       timestamptz,
  mfa_last_step        bigint NOT NULL DEFAULT 0,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, realm, email)
);

CREATE TABLE identity.user_roles (
  tenant_id uuid NOT NULL,
  user_id   uuid NOT NULL,
  role_code text NOT NULL REFERENCES identity.roles (code),
  PRIMARY KEY (tenant_id, user_id),
  FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id)
);

CREATE INDEX user_roles_role_idx ON identity.user_roles (tenant_id, role_code);

CREATE TABLE identity.recovery_codes (
  tenant_id uuid NOT NULL,
  id        uuid NOT NULL,
  user_id   uuid NOT NULL,
  code_hash bytea NOT NULL,
  used_at   timestamptz,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, user_id, code_hash),
  FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id)
);

-- Sessions.
CREATE TABLE identity.sessions (
  tenant_id      uuid NOT NULL,
  id             uuid NOT NULL,
  user_id        uuid NOT NULL,
  device_label   text NOT NULL,
  ip             text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_used_at   timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz NOT NULL,
  revoked_at     timestamptz,
  revoked_reason text,
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id)
);

CREATE INDEX sessions_user_active_idx ON identity.sessions (tenant_id, user_id, last_used_at DESC)
  WHERE revoked_at IS NULL;

CREATE TABLE identity.refresh_tokens (
  tenant_id  uuid NOT NULL,
  id         uuid NOT NULL,
  session_id uuid NOT NULL,
  token_hash bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  used_at    timestamptz,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, token_hash),
  FOREIGN KEY (tenant_id, session_id) REFERENCES identity.sessions (tenant_id, id)
);

CREATE INDEX refresh_tokens_session_idx ON identity.refresh_tokens (tenant_id, session_id);

-- Single-use tokens sent by e-mail.
CREATE TABLE identity.user_tokens (
  tenant_id  uuid NOT NULL,
  id         uuid NOT NULL,
  user_id    uuid NOT NULL,
  kind       text NOT NULL CHECK (kind IN ('email_verification', 'password_reset', 'invitation')),
  token_hash bytea NOT NULL,
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, token_hash),
  FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id)
);

CREATE INDEX user_tokens_user_idx ON identity.user_tokens (tenant_id, user_id, kind);

-- Second step of a login, created after the password is confirmed.
CREATE TABLE identity.mfa_challenges (
  tenant_id   uuid NOT NULL,
  id          uuid NOT NULL,
  user_id     uuid NOT NULL,
  token_hash  bytea NOT NULL,
  attempts    integer NOT NULL DEFAULT 0,
  expires_at  timestamptz NOT NULL,
  consumed_at timestamptz,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, token_hash),
  FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id)
);

CREATE TABLE identity.access_events (
  tenant_id   uuid NOT NULL REFERENCES core.tenants (id),
  id          uuid NOT NULL,
  user_id     uuid,
  realm       text NOT NULL,
  event       text NOT NULL,
  device      text NOT NULL,
  ip          text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id)
);

CREATE INDEX access_events_user_idx ON identity.access_events (tenant_id, user_id, occurred_at DESC);

-- Append-only trail: no role receives UPDATE or DELETE on this table.
CREATE TABLE audit.entries (
  tenant_id   uuid NOT NULL REFERENCES core.tenants (id),
  id          uuid NOT NULL,
  actor_type  text NOT NULL CHECK (actor_type IN ('user', 'operator', 'system')),
  actor_id    text NOT NULL,
  action      text NOT NULL,
  target_type text NOT NULL,
  target_id   text NOT NULL,
  ip          text NOT NULL DEFAULT '',
  trace_id    text NOT NULL DEFAULT '',
  metadata    jsonb NOT NULL DEFAULT '{}',
  occurred_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);

CREATE INDEX audit_entries_recent_idx ON audit.entries (tenant_id, occurred_at DESC, id DESC);

-- +goose StatementBegin
DO $$
DECLARE
  table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'identity.users', 'identity.user_roles', 'identity.recovery_codes', 'identity.sessions',
    'identity.refresh_tokens', 'identity.user_tokens', 'identity.mfa_challenges',
    'identity.access_events', 'audit.entries'
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

-- The operator tool only creates invitations and records what it did.
CREATE POLICY platform_access ON identity.users TO app_platform USING (true) WITH CHECK (true);
CREATE POLICY platform_access ON identity.user_roles TO app_platform USING (true) WITH CHECK (true);
CREATE POLICY platform_access ON identity.user_tokens TO app_platform USING (true) WITH CHECK (true);
CREATE POLICY platform_access ON audit.entries TO app_platform USING (true) WITH CHECK (true);

GRANT SELECT ON identity.permissions, identity.roles, identity.role_permissions TO app_runtime, app_platform;
GRANT SELECT, INSERT, UPDATE ON identity.users, identity.sessions, identity.refresh_tokens,
  identity.user_tokens, identity.mfa_challenges TO app_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON identity.user_roles, identity.recovery_codes TO app_runtime;
GRANT SELECT, INSERT ON identity.access_events, audit.entries TO app_runtime;
GRANT SELECT, INSERT ON identity.users, audit.entries TO app_platform;
GRANT SELECT, INSERT, UPDATE ON identity.user_roles TO app_platform;
GRANT SELECT, INSERT, UPDATE ON identity.user_tokens TO app_platform;

-- +goose Down
DROP SCHEMA audit CASCADE;
DROP SCHEMA identity CASCADE;
DROP FUNCTION core.tenant_is_active(uuid);
DROP FUNCTION core.resolve_tenant_by_slug(text);
CREATE POLICY migration_seed ON core.tenants FOR DELETE TO app_owner
  USING (id = '01960000-0000-7000-8000-000000000001');
DELETE FROM core.tenants WHERE id = '01960000-0000-7000-8000-000000000001';
DROP POLICY migration_seed ON core.tenants;
