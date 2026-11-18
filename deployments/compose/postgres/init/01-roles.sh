#!/bin/sh
# Creates the least-privilege database roles on the first start of an empty volume.
set -eu

: "${MIGRATION_DB_PASSWORD:?MIGRATION_DB_PASSWORD is required}"
: "${DB_PASSWORD:?DB_PASSWORD is required}"

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  -v owner_password="$MIGRATION_DB_PASSWORD" \
  -v runtime_password="$DB_PASSWORD" \
  -v database_name="$POSTGRES_DB" <<'SQL'
CREATE ROLE app_owner LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'owner_password';
CREATE ROLE app_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'runtime_password';

ALTER DATABASE :"database_name" OWNER TO app_owner;
REVOKE ALL ON DATABASE :"database_name" FROM PUBLIC;
GRANT CONNECT ON DATABASE :"database_name" TO app_runtime;

ALTER SCHEMA public OWNER TO app_owner;
REVOKE ALL ON SCHEMA public FROM PUBLIC;

ALTER ROLE app_runtime SET statement_timeout = '10s';
ALTER ROLE app_runtime SET lock_timeout = '3s';
ALTER ROLE app_runtime SET idle_in_transaction_session_timeout = '15s';
SQL
