#!/usr/bin/env bash
# Restore drill: dumps the running database, restores it into a new PostgreSQL
# and proves the copy is complete and still isolates stores. It prints the time
# of each step for docs/runbooks/backup-restore.md.
set -euo pipefail
export MSYS_NO_PATHCONV=1
cd "$(dirname "$0")/.."

IMAGE=postgres:18.7-alpine
DRILL=ecommerce-restore-drill
WORK=$(mktemp -d)
trap 'docker rm -f "$DRILL" >/dev/null 2>&1 || true; rm -rf "$WORK"' EXIT
set -a; source .env; set +a

now() { date +%s.%N; }
elapsed() { awk -v a="$1" -v b="$(now)" 'BEGIN { printf "%.1f", b - a }'; }
psql_source() { docker compose exec -T postgres psql -U postgres -d ecommerce -At -c "$1"; }
psql_drill() { docker exec "$DRILL" psql -U postgres -d ecommerce -At -c "$1"; }

# Tables compared between the source and the copy.
CHECKS="SELECT 'tenants', count(*) FROM core.tenants
UNION ALL SELECT 'users', count(*) FROM identity.users
UNION ALL SELECT 'products', count(*) FROM catalog.products
UNION ALL SELECT 'orders', count(*) FROM orders.orders
UNION ALL SELECT 'payments', count(*) FROM payments.payments
UNION ALL SELECT 'audit', count(*) FROM audit.entries
UNION ALL SELECT 'outbox', count(*) FROM messaging.outbox
UNION ALL SELECT 'migration', max(version_id) FROM goose_db_version
ORDER BY 1"

echo "1. Backup (pg_dump, formato custom)"
start=$(now)
docker compose exec -T postgres pg_dump -U postgres -d ecommerce -Fc --no-comments > "$WORK/ecommerce.dump"
expected=$(psql_source "$CHECKS")
echo "   $(du -h "$WORK/ecommerce.dump" | cut -f1) em $(elapsed "$start") s"

echo "2. Novo servidor com os papéis da aplicação"
start=$(now)
docker run -d --name "$DRILL" -e POSTGRES_PASSWORD="$POSTGRES_SUPERUSER_PASSWORD" -e POSTGRES_DB=ecommerce \
  -e MIGRATION_DB_PASSWORD -e DB_PASSWORD -e PLATFORM_DB_PASSWORD \
  -v "$PWD/deployments/compose/postgres/init:/docker-entrypoint-initdb.d:ro" "$IMAGE" >/dev/null
# The image starts a temporary server to run the init scripts, then the real one.
until [[ $(docker logs "$DRILL" 2>&1 | grep -c 'ready to accept connections') -ge 2 ]]; do sleep 1; done
echo "   pronto em $(elapsed "$start") s"

echo "3. Restauração"
start=$(now)
docker exec -i "$DRILL" pg_restore -U postgres -d ecommerce --exit-on-error --single-transaction < "$WORK/ecommerce.dump"
restore_time=$(elapsed "$start")
echo "   restaurado em $restore_time s"

echo "4. Verificação"
failures=0
actual=$(psql_drill "$CHECKS")
if [[ "$actual" == "$expected" ]]; then echo "   PASS  contagens e versão das migrations iguais à origem"; else echo "   FAIL  contagens diferentes"; diff <(echo "$expected") <(echo "$actual") || true; failures=$((failures + 1)); fi
unforced=$(psql_drill "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id' WHERE c.relkind = 'r' AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND NOT c.relforcerowsecurity")
if [[ "$unforced" == "0" ]]; then echo "   PASS  RLS forçado em todas as tabelas com tenant_id"; else echo "   FAIL  $unforced tabelas sem RLS forçado"; failures=$((failures + 1)); fi
leaked=$(docker exec -e PGPASSWORD="$DB_PASSWORD" "$DRILL" psql -h 127.0.0.1 -U app_runtime -d ecommerce -At -c "SELECT count(*) FROM orders.orders")
if [[ "$leaked" == "0" ]]; then echo "   PASS  papel da aplicação sem loja não lê pedidos"; else echo "   FAIL  papel da aplicação leu $leaked pedidos sem loja"; failures=$((failures + 1)); fi
bypass=$(psql_drill "SELECT count(*) FROM pg_roles WHERE rolname LIKE 'app_%' AND (rolsuper OR rolbypassrls)")
if [[ "$bypass" == "0" ]]; then echo "   PASS  nenhum papel da aplicação ignora RLS"; else echo "   FAIL  papel com bypass"; failures=$((failures + 1)); fi

echo
if (( failures > 0 )); then echo "Ensaio falhou ($failures)"; exit 1; fi
echo "Ensaio concluído: restauração em $restore_time s"
