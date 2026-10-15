#!/bin/bash
# Imports (or overwrites) dashboards/balance-alert-dashboard.json into an existing Grafana; needs jq.
# Usage: ./import-dashboard.sh [grafana-url] [username] [password]
#        The password may also come from the GRAFANA_ADMIN_PASSWORD environment variable.
set -euo pipefail
cd "$(dirname "$0")"

URL="${1:-http://localhost:3000}"
USER="${2:-${GRAFANA_ADMIN_USER:-admin}}"
PASS="${3:-${GRAFANA_ADMIN_PASSWORD:-}}"
[ -n "$PASS" ] || { echo "Missing Grafana Localized messageïLocalized message 3 Localized messageparameterLocalized message GRAFANA_ADMIN_PASSWORD" >&2; exit 1; }
command -v jq >/dev/null || { echo "Requires Localized message jq" >&2; exit 1; }

DS_UID=$(curl -sf -u "$USER:$PASS" "$URL/api/datasources/name/Prometheus" | jq -r '.uid // empty')
[ -n "$DS_UID" ] || { echo "Grafana Localized messageNo Localized message Prometheus Localized messagedataLocalized messageïLocalized messageAdd" >&2; exit 1; }

jq --arg uid "$DS_UID" \
   'walk(if type == "object" and .uid == "${DS_PROMETHEUS}" then .uid = $uid else . end)
    | {dashboard: ., overwrite: true, folderUid: ""}' \
   dashboards/quotapulse-dashboard.json \
| curl -sf -u "$USER:$PASS" -H "Content-Type: application/json" -d @- "$URL/api/dashboards/db" \
| jq -r --arg url "$URL" '"Already Localized message: " + $url + .url'
