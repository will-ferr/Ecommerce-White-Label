#!/usr/bin/env bash
# Verifies the running Compose environment end to end.
set -uo pipefail

# Git Bash on Windows would otherwise rewrite URL paths inside arguments.
export MSYS_NO_PATHCONV=1

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
failures=0

pass() { printf 'PASS  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; failures=$((failures + 1)); }

expect() { # name, expected text, actual text
  if [[ "$3" == *"$2"* ]]; then pass "$1"; else fail "$1 (esperado: $2; recebido: ${3:0:200})"; fi
}

# Retries until the telemetry pipeline has delivered the data.
eventually() { # name, command
  local name="$1"; shift
  for _ in $(seq 1 30); do
    if "$@" >/dev/null 2>&1; then pass "$name"; return; fi
    sleep 3
  done
  fail "$name"
}

status_of() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

expect "API responde pela borda" '"status":"ok"' "$(curl -s "$BASE_URL/api/v1/status")"

headers="$(curl -s -D - -o /dev/null "$BASE_URL/api/v1/status")"
expect "API devolve correlation id" "x-correlation-id" "${headers,,}"
expect "API aplica cabeçalhos de segurança" "x-content-type-options: nosniff" "${headers,,}"

missing="$(curl -s -i "$BASE_URL/api/v1/inexistente")"
expect "rota inexistente usa RFC 9457" "application/problem+json" "${missing,,}"
expect "erro informa o código" '"code":"ROUTE_NOT_FOUND"' "$missing"

expect "probes não ficam expostos na borda" "404" "$(status_of "$BASE_URL/health/ready")"

aurora="$(curl -s -H 'Host: aurora.localhost' "$BASE_URL/")"
terra="$(curl -s -H 'Host: terra.localhost' "$BASE_URL/")"
expect "loja Aurora é renderizada no servidor" "Aurora Cosméticos" "$aurora"
expect "loja Aurora recebe as próprias cores" "--wl-color-primary:#8a1c5c" "$aurora"
expect "loja Terra é renderizada no servidor" "Terra Café" "$terra"
expect "loja Terra recebe as próprias cores" "--wl-color-primary:#8a4b12" "$terra"
if [[ "$aurora" == *"Terra Café"* || "$terra" == *"Aurora Cosméticos"* ]]; then fail "lojas misturam conteúdo"; else pass "lojas não misturam conteúdo"; fi

expect "API identifica a loja pelo domínio" '"slug":"terra"' "$(curl -s -H 'Host: terra.localhost' "$BASE_URL/api/v1/storefront/config")"
expect "cabeçalho do cliente não troca a loja" '"slug":"terra"' "$(curl -s -H 'Host: terra.localhost' -H 'X-Tenant-ID: aurora' -H 'X-Forwarded-Host: aurora.localhost' "$BASE_URL/api/v1/storefront/config")"
expect "domínio desconhecido não tem loja na API" "STORE_NOT_FOUND" "$(curl -s -H 'Host: desconhecida.localhost' "$BASE_URL/api/v1/storefront/config")"
expect "domínio desconhecido responde 404 na loja" "404" "$(status_of -H 'Host: desconhecida.localhost' "$BASE_URL/")"
expect "painel da loja responde" "Admin da loja" "$(curl -s -H 'Host: admin.localhost' "$BASE_URL/")"
expect "console da plataforma responde" "Console da plataforma" "$(curl -s -H 'Host: console.localhost' "$BASE_URL/")"

panel_headers="$(curl -s -D - -o /dev/null -H 'Host: admin.localhost' "$BASE_URL/")"
expect "painel aplica CSP" "content-security-policy: default-src 'self'" "${panel_headers,,}"
expect "SPA atende rotas internas" "200" "$(status_of -H 'Host: admin.localhost' "$BASE_URL/produtos/1")"

for _ in $(seq 1 10); do curl -s -o /dev/null "$BASE_URL/api/v1/status"; done

has_metric() { curl -sf -G http://127.0.0.1:9090/api/v1/query --data-urlencode "query=$1" | grep -q '"value"'; }
has_trace() { curl -sf -G http://127.0.0.1:3200/api/search --data-urlencode 'q={ resource.service.name = "ecommerce-api" && name = "GET /api/v1/status" }' | grep -q traceID; }
has_db_span() {
  local id
  id="$(curl -sf -G http://127.0.0.1:3200/api/search --data-urlencode 'q={ resource.service.name = "ecommerce-api" && name = "GET /api/v1/status" }' | grep -o '"traceID":"[^"]*"' | head -1 | cut -d'"' -f4)"
  [[ -n "$id" ]] && curl -sf "http://127.0.0.1:3200/api/traces/$id" | grep -qiE 'postgres|pool\.acquire|db\.system'
}
has_log() {
  curl -sf -G http://127.0.0.1:3100/loki/api/v1/query_range \
    --data-urlencode 'query={service_name="ecommerce-api"} |= "http.request"' \
    --data-urlencode 'limit=1' | grep -q 'http.request'
}

eventually "métrica HTTP chega ao Prometheus" has_metric 'http_server_request_duration_seconds_count{http_route="/api/v1/status"}'
eventually "métrica do pool chega ao Prometheus" has_metric '{__name__=~"db_.*connection.*|pgxpool_.*"}'
eventually "métrica do runtime Go chega ao Prometheus" has_metric '{__name__=~"go_goroutine.*"}'
eventually "trace chega ao Tempo" has_trace
eventually "trace contém a consulta ao banco" has_db_span
eventually "log chega ao Loki" has_log

expect "rota protegida exige login" "AUTHENTICATION_REQUIRED" "$(curl -s -H 'Host: aurora.localhost' "$BASE_URL/api/v1/auth/me")"
expect "equipe exige login" "AUTHENTICATION_REQUIRED" "$(curl -s -H 'Host: admin.localhost' "$BASE_URL/api/v1/staff/team")"
expect "console exige login" "AUTHENTICATION_REQUIRED" "$(curl -s -H 'Host: console.localhost' "$BASE_URL/api/v1/platform/audit")"
expect "renovação de sessão recusa outro site" "ORIGIN_NOT_ALLOWED" "$(curl -s -X POST -H 'Host: aurora.localhost' -H 'Origin: https://evil.test' "$BASE_URL/api/v1/auth/refresh")"

labels="$(curl -s -G http://127.0.0.1:9090/api/v1/series --data-urlencode 'match[]=http_server_request_duration_seconds_count')"
if [[ "$labels" == *"aurora"* || "$labels" == *"terra"* ]]; then fail "host do tenant virou rótulo de métrica"; else pass "host do tenant não vira rótulo de métrica"; fi

if (( failures > 0 )); then
  printf '\n%d verificação(ões) falharam\n' "$failures"
  exit 1
fi
printf '\nTodas as verificações passaram\n'
