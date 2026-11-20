#!/usr/bin/env bash
# OWASP ZAP scans of the running environment: a passive scan of the store and
# the panels, and an active scan of the store API from its OpenAPI contract.
# Fails on any high-risk finding. Reports are written to security-reports/.
set -euo pipefail
export MSYS_NO_PATHCONV=1
cd "$(dirname "$0")/.."

ZAP=ghcr.io/zaproxy/zaproxy:2.16.1
NETWORK=ecommerce-white-label_default
EDGE=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' ecommerce-white-label-edge-1)
mkdir -p security-reports
cp docs/api/openapi.yaml security-reports/openapi.yaml
chmod 777 security-reports

zap() { # host, command...
  local host="$1"; shift
  docker run --rm --network "$NETWORK" --add-host "$host.localhost:$EDGE" -v "$PWD/security-reports:/zap/wrk:rw" "$ZAP" "$@" -I >/dev/null
}

for host in aurora admin console; do
  zap "$host" zap-baseline.py -t "http://$host.localhost:8080/" -m 2 -J "baseline-$host.json"
done
zap aurora zap-api-scan.py -t openapi.yaml -f openapi -O http://aurora.localhost:8080 -J api.json

node -e '
const fs = require("fs")
const risk = { 0: "Info", 1: "Baixo", 2: "Médio", 3: "Alto" }
let high = 0
for (const file of fs.readdirSync("security-reports").filter((name) => name.endsWith(".json"))) {
  const report = JSON.parse(fs.readFileSync(`security-reports/${file}`, "utf8"))
  for (const site of report.site) for (const alert of site.alerts) {
    if (Number(alert.riskcode) >= 2) console.log(`${file}: ${risk[alert.riskcode]} - ${alert.alert} (${alert.count})`)
    if (Number(alert.riskcode) >= 3) high++
  }
}
if (high > 0) { console.error(`${high} achados de risco alto`); process.exit(1) }
console.log("Nenhum achado de risco alto")
'
