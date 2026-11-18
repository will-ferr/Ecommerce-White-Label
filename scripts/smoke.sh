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

store="$(curl -s -H 'Host: loja-teste.localhost' "$BASE_URL/")"
expect "loja entrega HTML renderizado no servidor" "Loja em preparação" "$store"
expect "loja renderiza o estado da API no servidor" "Operacional" "$store"
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

labels="$(curl -s -G http://127.0.0.1:9090/api/v1/series --data-urlencode 'match[]=http_server_request_duration_seconds_count')"
if [[ "$labels" == *"loja-teste"* ]]; then fail "host do tenant virou rótulo de métrica"; else pass "host do tenant não vira rótulo de métrica"; fi

if (( failures > 0 )); then
  printf '\n%d verificação(ões) falharam\n' "$failures"
  exit 1
fi
printf '\nTodas as verificações passaram\n'
