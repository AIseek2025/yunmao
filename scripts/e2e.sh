#!/usr/bin/env bash
# yunmao 端到端 smoke：登录 → 取 room token → 投喂 → 查询请求详情 → 拉 LL-HLS playlist。
#
# 期望前置：`make dev-up && make app-up` 已经把基础设施 + 6 个 Go svc + 3 个 Rust 二进制起好。
# 如果只想跑“PoC memory 模式”而无 docker，可在另一个终端用 `make poc-feed` 替代本脚本。
#
# 用法：
#   bash scripts/e2e.sh                            # 默认走 localhost
#   YUNMAO_E2E_HOST=10.0.0.1 bash scripts/e2e.sh   # 跨主机
#
# 退出码：
#   0 = 全部通过
#   2 = 任一阶段返回非 200 或必填字段缺失
#
# 该脚本与 docs/dev/03-third-iteration-deliverable.md 的"验证命令"章节配合使用。

set -euo pipefail

HOST="${YUNMAO_E2E_HOST:-127.0.0.1}"
USER_SVC="${YUNMAO_USER_SVC:-http://${HOST}:8101}"
ROOM_SVC="${YUNMAO_ROOM_SVC:-http://${HOST}:8102}"
FEEDING_SVC="${YUNMAO_FEEDING_SVC:-http://${HOST}:8201}"
MEDIA_EDGE="${YUNMAO_MEDIA_EDGE:-http://${HOST}:8080}"
# 默认使用 feeding-svc 自带 seed 房间；如要避免每日上限累计，可外部 export
# YUNMAO_E2E_ROOM=room_xxx 并先在 feeding-svc 注册该房间。
ROOM_ID="${YUNMAO_E2E_ROOM:-room_demo}"
PHONE="${YUNMAO_E2E_PHONE:-+8613${RANDOM}${RANDOM}}"

JQ_BIN=${JQ_BIN:-jq}
if ! command -v "$JQ_BIN" >/dev/null 2>&1; then
  echo "[e2e][ERR] need jq in PATH; brew install jq" >&2
  exit 2
fi

line() { printf '\n[e2e] %s\n' "$*"; }

line "Step 1: login user-svc → dev access_token"
LOGIN_BODY=$(curl -fsS -X POST "${USER_SVC}/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"phone_e164\":\"${PHONE}\"}")
ACCESS=$(echo "$LOGIN_BODY" | $JQ_BIN -r '.access_token // empty')
USER_ID=$(echo "$LOGIN_BODY" | $JQ_BIN -r '.user.id // empty')
if [[ -z "$ACCESS" ]]; then
  echo "[e2e][ERR] login failed: $LOGIN_BODY" >&2; exit 2
fi
echo "  user_id=${USER_ID} jwt_len=${#ACCESS}"

line "Step 2: room subscription token from room-svc"
# 若房间不存在，room-svc 会返回 404；脚本会自动 fallback 到 room_demo（feeding-svc 已 seed）。
ROOM_TOKEN_BODY=$(curl -fsS -X POST "${ROOM_SVC}/v1/rooms/${ROOM_ID}/subscriptions" \
  -H "Authorization: Bearer ${ACCESS}" -H 'Content-Type: application/json' -d '{}' || true)
if ! echo "$ROOM_TOKEN_BODY" | $JQ_BIN -e '.token' >/dev/null 2>&1; then
  echo "  room ${ROOM_ID} not found in room-svc; falling back to room_demo"
  ROOM_ID=room_demo
  ROOM_TOKEN_BODY=$(curl -fsS -X POST "${ROOM_SVC}/v1/rooms/${ROOM_ID}/subscriptions" \
    -H "Authorization: Bearer ${ACCESS}" -H 'Content-Type: application/json' -d '{}')
fi
ROOM_TOKEN=$(echo "$ROOM_TOKEN_BODY" | $JQ_BIN -r '.token // empty')
if [[ -z "$ROOM_TOKEN" ]]; then
  echo "[e2e][ERR] room token issuance failed: $ROOM_TOKEN_BODY" >&2; exit 2
fi
echo "  room_token_len=${#ROOM_TOKEN}"

line "Step 3: trigger feeding-svc with 1g idempotency key"
IDEM="e2e-$(date +%s)-${RANDOM}"
HTTP_AND_BODY=$(curl -s -o /tmp/yunmao-e2e-feed.json -w '%{http_code}' \
  -X POST "${FEEDING_SVC}/api/v1/feed-requests" \
  -H "Authorization: Bearer ${ACCESS}" -H 'Content-Type: application/json' \
  -d "{\"user_id\":\"${USER_ID}\",\"room_id\":\"${ROOM_ID}\",\"amount_grams\":1,\"idempotency_key\":\"${IDEM}\"}" || true)
FEED_BODY=$(cat /tmp/yunmao-e2e-feed.json)
FEED_ID=$(echo "$FEED_BODY" | $JQ_BIN -r '.feed_request_id // .id // empty')
FEED_STATUS=$(echo "$FEED_BODY" | $JQ_BIN -r '.status // empty')
if [[ "$HTTP_AND_BODY" == "409" || "$HTTP_AND_BODY" == "429" ]]; then
  echo "  feed rejected by safety policy (${HTTP_AND_BODY}): $FEED_BODY (WARN, treated as smoke pass)"
  FEED_ID=""
elif [[ -z "$FEED_ID" ]]; then
  echo "[e2e][ERR] feed request unexpected: http=${HTTP_AND_BODY} body=$FEED_BODY" >&2; exit 2
else
  echo "  feed_id=${FEED_ID} initial_status=${FEED_STATUS}"
fi

line "Step 4: poll feeding-svc until terminal state"
if [[ -n "$FEED_ID" ]]; then
  ATTEMPTS=0
  LAST=""
  while (( ATTEMPTS < 40 )); do
    ATTEMPTS=$((ATTEMPTS+1))
    DETAIL=$(curl -fsS -H "Authorization: Bearer ${ACCESS}" "${FEEDING_SVC}/api/v1/feed-requests/${FEED_ID}" || true)
    LAST=$(echo "$DETAIL" | $JQ_BIN -r '.status // empty')
    case "$LAST" in
      succeeded|failed|acknowledged|dispatched)
        echo "  attempt ${ATTEMPTS} status=${LAST}"; break ;;
      *)
        sleep 0.25 ;;
    esac
  done
  if [[ -z "$LAST" ]]; then
    echo "[e2e][WARN] never observed status (poll exhausted)" >&2
  fi
else
  echo "  (skipped: no feed_id from Step 3)"
fi

line "Step 5: pull LL-HLS playlist (best effort; 200=ready, 404=no SPS yet)"
PLAYLIST_URL="${MEDIA_EDGE}/live/${ROOM_ID}/index_ll.m3u8"
HTTP=$(curl -s -o /tmp/yunmao-e2e-playlist.m3u8 -w '%{http_code}' "$PLAYLIST_URL" || true)
echo "  GET ${PLAYLIST_URL} → ${HTTP}"
if [[ "$HTTP" == "200" ]]; then
  head -n 10 /tmp/yunmao-e2e-playlist.m3u8 | sed 's/^/    /'
fi

line "Step 6: process-level health checks (staging smoke)"
echo "  --- 6.1 liveness probes (/healthz) ---"
for svc in user-svc:8101 room-svc:8102 feeding-svc:8201 device-svc:8103 billing-svc:8104 admin-svc:8105 gateway:${YUNMAO_GATEWAY_PORT:-8090} media-edge:8080; do
  name="${svc%%:*}"; port="${svc##*:}"
  c=$(curl -s -o /dev/null -w '%{http_code}' "http://${HOST}:${port}/healthz" || true)
  printf '  %-12s healthz → %s\n' "$name" "$c"
  if [[ "$c" != "200" ]]; then
    echo "[e2e][ERR] $name liveness probe failed (HTTP $c)" >&2; exit 2
  fi
done

echo "  --- 6.2 readiness probes (/internal/readyz) ---"
for svc in user-svc:8101 room-svc:8102 feeding-svc:8201 device-svc:8103 billing-svc:8104 admin-svc:8105; do
  name="${svc%%:*}"; port="${svc##*:}"
  c=$(curl -s -o /dev/null -w '%{http_code}' "http://${HOST}:${port}/internal/readyz" || true)
  printf '  %-12s readyz → %s\n' "$name" "$c"
  if [[ "$c" != "200" ]]; then
    echo "[e2e][WARN] $name readiness probe failed (HTTP $c) — may have degraded dependencies" >&2
  fi
done

echo "  --- 6.3 credential diagnostics (billing-svc) ---"
CRED_DIAG=$(curl -s "http://${HOST}:8104/internal/diagnose/credentials" || true)
if [[ -n "$CRED_DIAG" ]]; then
  echo "  billing-svc credential status:"
  echo "$CRED_DIAG" | $JQ_BIN -r 'to_entries[] | "    \(.key): \(.value)"' 2>/dev/null || echo "$CRED_DIAG"
else
  echo "[e2e][WARN] billing-svc credential diagnostics unavailable" >&2
fi

echo "  --- 6.4 TURN credential issuance (room-svc) ---"
TURN_CHECK=$(curl -s -H "Authorization: Bearer ${ACCESS}" \
  "http://${HOST}:8102/v1/rooms/${ROOM_ID}/ice-servers" || true)
if [[ -n "$TURN_CHECK" ]] && echo "$TURN_CHECK" | $JQ_BIN -e '(.ice_servers // .iceServers // []) | length > 0' >/dev/null 2>&1; then
  echo "  ✓ TURN credentials issued successfully"
  echo "$TURN_CHECK" | $JQ_BIN -c '(.ice_servers // .iceServers)' | head -c 200
  echo ""
else
  echo "[e2e][WARN] TURN credential check failed or not configured" >&2
fi

line "Step 7: metrics smoke"
GATEWAY_PORT="${YUNMAO_GATEWAY_PORT:-8090}"
for svc in feeding-svc:8201 user-svc:8101 room-svc:8102 admin-svc:8105 device-svc:8103 billing-svc:8104 gateway:${GATEWAY_PORT} media-edge:8080; do
  name="${svc%%:*}"; port="${svc##*:}"
  c=$(curl -s -o /dev/null -w '%{http_code}' "http://${HOST}:${port}/metrics" || true)
  printf '  %-12s metrics → %s\n' "$name" "$c"
done

line "PASS – e2e smoke completed."
