#!/usr/bin/env bash
# 投喂端到端 PoC 触发脚本。
# 假设 feeding-svc / device-edge / gateway 都已跑起来。
# 流程：POST /api/v1/feed-requests → feeding-svc 触发 device-edge → device-edge 回调 ack →
#       feeding-svc 通过 gateway 把 feeding.* 事件扇出到房间订阅者。
set -euo pipefail

FEED_URL=${FEED_URL:-http://localhost:8201/api/v1/feed-requests}
USER_ID=${USER_ID:-usr_demo}
ROOM_ID=${ROOM_ID:-room_demo}
AMOUNT=${AMOUNT:-5}
IDEM=${IDEM:-$(date +%s%N)}

curl -fsS -X POST "$FEED_URL" \
  -H 'Content-Type: application/json' \
  -H "X-Idempotency-Key: $IDEM" \
  -d @- <<JSON
{
  "user_id": "$USER_ID",
  "room_id": "$ROOM_ID",
  "amount_grams": $AMOUNT,
  "idempotency_key": "$IDEM"
}
JSON

echo
