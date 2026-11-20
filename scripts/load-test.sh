#!/usr/bin/env bash
# Measures the capacity of the local environment with k6 (docs/observability/capacity.md).
# Usage: bash scripts/load-test.sh [peak browsing iterations per second] [orders per second]
set -euo pipefail
export MSYS_NO_PATHCONV=1

cd "$(dirname "$0")/.."
PEAK_RPS="${1:-150}"
ORDERS_PER_SECOND="${2:-3}"
STORES=($(seq -f "teste-carga-%g" 1 16))

node tests/load/prepare.mjs "${STORES[@]}"

hosts=$(printf '%s.localhost,' "${STORES[@]}")
mkdir -p tests/load/results
docker run --rm --network ecommerce-white-label_default \
  -v "$PWD/tests/load:/load" \
  -e BASE_URL=http://edge:8080 -e STORE_HOSTS="${hosts%,}" \
  -e PEAK_RPS="$PEAK_RPS" -e ORDERS_PER_SECOND="$ORDERS_PER_SECOND" \
  grafana/k6:1.0.0 run --summary-export "/load/results/summary-${PEAK_RPS}.json" /load/storefront.js
