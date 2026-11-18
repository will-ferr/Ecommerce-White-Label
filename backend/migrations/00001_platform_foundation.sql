-- +goose Up
CREATE SCHEMA app;

GRANT USAGE ON SCHEMA app TO app_runtime;

-- +goose StatementBegin
-- Tenant of the current transaction. Returns NULL when no tenant is set, which
-- makes every Row Level Security policy match zero rows.
CREATE FUNCTION app.current_tenant_id() RETURNS uuid
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
  SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION app.current_tenant_id() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION app.current_tenant_id() TO app_runtime;

-- +goose Down
DROP FUNCTION app.current_tenant_id();
DROP SCHEMA app;
