#!/bin/sh
# Creates or updates the least-privilege database roles. It is safe to run on
# every start, so existing volumes also receive new roles.
set -eu

: "${MIGRATION_DB_PASSWORD:?MIGRATION_DB_PASSWORD is required}"
: "${DB_PASSWORD:?DB_PASSWORD is required}"
: "${PLATFORM_DB_PASSWORD:?PLATFORM_DB_PASSWORD is required}"

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  -v owner_password="$MIGRATION_DB_PASSWORD" \
  -v runtime_password="$DB_PASSWORD" \
  -v platform_password="$PLATFORM_DB_PASSWORD" \
  -v database_name="$POSTGRES_DB" <<'SQL'
SELECT format('CREATE ROLE %I', role_name)
FROM unnest(ARRAY['app_owner', 'app_runtime', 'app_platform']) AS role_name
WHERE NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = role_name)
\gexec

ALTER ROLE app_owner LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'owner_password';
ALTER ROLE app_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'runtime_password';
ALTER ROLE app_platform LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'platform_password';

ALTER DATABASE :"database_name" OWNER TO app_owner;
REVOKE ALL ON DATABASE :"database_name" FROM PUBLIC;
GRANT CONNECT ON DATABASE :"database_name" TO app_runtime, app_platform;

ALTER SCHEMA public OWNER TO app_owner;
REVOKE ALL ON SCHEMA public FROM PUBLIC;

ALTER ROLE app_runtime SET statement_timeout = '10s';
ALTER ROLE app_runtime SET lock_timeout = '3s';
ALTER ROLE app_runtime SET idle_in_transaction_session_timeout = '15s';
ALTER ROLE app_platform SET statement_timeout = '30s';
ALTER ROLE app_platform SET lock_timeout = '5s';
ALTER ROLE app_platform SET idle_in_transaction_session_timeout = '30s';
SQL
