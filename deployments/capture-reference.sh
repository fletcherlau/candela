#!/bin/sh
# Run externally at 14:45 Asia/Shanghai; the backend validates the trading day.
set -eu
: "${SYNC_API_KEY:?SYNC_API_KEY is required}"
capture_date=$(TZ=Asia/Shanghai date +%Y%m%d)
printf 'header = "X-Api-Key: %s"\n' "$SYNC_API_KEY" |
  curl --config - --silent --show-error --fail-with-body --max-time 15 \
    --header 'Content-Type: application/json' \
    --data "{\"tradeDate\":\"$capture_date\"}" \
    http://127.0.0.1:8888/api/v1/rotation/reference-captures
