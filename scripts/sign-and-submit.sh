#!/usr/bin/env bash
# Submit a sample notification with a valid HMAC signature. Customize via env.
set -euo pipefail

APP_KEY="${APP_KEY:-demo-app}"
APP_SECRET="${APP_SECRET:-demo-secret-please-change}"
HOST="${HOST:-http://localhost:8080}"
TARGET_PATH="${TARGET_PATH:-/v1/notifications}"
EVENT_ID="${EVENT_ID:-evt-$(date +%s)-$$}"
BODY="${BODY:-{\"event_id\":\"${EVENT_ID}\",\"channel\":\"email\",\"recipient\":{\"user_id\":1},\"template_id\":\"11111111-1111-1111-1111-111111111111\",\"variables\":{\"Name\":\"Demo\",\"Product\":\"NotifEngine\"}}}"

TS=$(date +%s)
TO_SIGN=$(printf "%s\n%s\n%s\n%s" "$TS" "POST" "$TARGET_PATH" "$BODY")
SIG=$(printf "%s" "$TO_SIGN" | openssl dgst -sha256 -hmac "$APP_SECRET" | awk '{print $2}')

echo "POST $HOST$TARGET_PATH"
curl -sS -X POST "$HOST$TARGET_PATH" \
  -H "Content-Type: application/json" \
  -H "X-App-Key: $APP_KEY" \
  -H "X-App-Timestamp: $TS" \
  -H "X-App-Signature: $SIG" \
  -d "$BODY"
echo
