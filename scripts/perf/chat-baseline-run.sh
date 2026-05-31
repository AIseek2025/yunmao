#!/usr/bin/env bash
# chat-baseline-run.sh：实际执行 N 用户发弹幕 + M 观众订阅；
# 输出 P50/P95/P99、ratelimit 拒绝率、moderation 调用比例。
#
# 调用：由 chat-baseline-up.sh 调起；
#      也可独立运行（要求 chat-svc/gateway 已起）。
#
# 实现说明：
# - 当前 PoC 简化为顺序 curl + 并发 token-bucket；
#   生产级建议用 vegeta / k6 / gatling，下一轮替换。
set -euo pipefail

USERS=${YUNMAO_CHAT_USERS:-1000}
VIEWERS=${YUNMAO_CHAT_VIEWERS:-5000}
CHAT_BASE=${YUNMAO_CHAT_BASE:-http://127.0.0.1:8204}
ROOM=${YUNMAO_CHAT_ROOM:-baseline_room}
PER_USER_MSGS=${YUNMAO_CHAT_PER_USER:-5}

if ! command -v curl >/dev/null 2>&1; then
  echo "curl required" >&2
  exit 1
fi

if ! curl -fsS "$CHAT_BASE/healthz" >/dev/null 2>&1; then
  echo "[chat-baseline-run] chat-svc not ready at $CHAT_BASE; aborting"
  echo "{\"status\":\"aborted\",\"reason\":\"chat-svc unreachable\"}"
  exit 0
fi

START=$(date +%s)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "[chat-baseline-run] users=$USERS viewers=$VIEWERS msgs_per_user=$PER_USER_MSGS"

# 模拟发送：使用 GNU parallel 时并发更大；这里走 background curl + wait。
SUCCESS=0
RATELIMIT=0
MODERATION_HIT=0
LATENCIES_FILE="$TMP/lat.txt"

submit() {
  local uid=$1 msg=$2
  local t0 t1 code body lat
  t0=$(python3 -c 'import time;print(int(time.time()*1000))')
  body=$(curl -fsS -o "$TMP/out.$$" -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -H "X-User-Id: u_$uid" \
    -d "{\"user_id\":\"u_$uid\",\"room_id\":\"$ROOM\",\"body\":\"$msg\"}" \
    "$CHAT_BASE/api/v1/rooms/$ROOM/chat" 2>/dev/null || true)
  code=${body:-000}
  t1=$(python3 -c 'import time;print(int(time.time()*1000))')
  lat=$((t1 - t0))
  echo "$lat" >>"$LATENCIES_FILE"
  case "$code" in
    200|201) SUCCESS=$((SUCCESS+1)) ;;
    429)     RATELIMIT=$((RATELIMIT+1)) ;;
  esac
  if grep -q 'flagged' "$TMP/out.$$" 2>/dev/null; then
    MODERATION_HIT=$((MODERATION_HIT+1))
  fi
  rm -f "$TMP/out.$$"
}

# 限制并发；本机走 100，Linux runner 走 1000+
CONC=${YUNMAO_CHAT_CONC:-100}
PENDING=0
for ((u=0; u<USERS; u++)); do
  for ((m=0; m<PER_USER_MSGS; m++)); do
    submit "$u" "perf-msg-$u-$m" &
    PENDING=$((PENDING+1))
    if [ "$PENDING" -ge "$CONC" ]; then
      wait -n
      PENDING=$((PENDING-1))
    fi
  done
done
wait

END=$(date +%s)
DUR=$((END - START))
TOTAL=$((USERS * PER_USER_MSGS))

# 统计
if command -v sort >/dev/null && [ -s "$LATENCIES_FILE" ]; then
  sort -n "$LATENCIES_FILE" >"$TMP/sorted.txt"
  N=$(wc -l <"$TMP/sorted.txt")
  P50_LINE=$(( (N * 50 + 99) / 100 ))
  P95_LINE=$(( (N * 95 + 99) / 100 ))
  P99_LINE=$(( (N * 99 + 99) / 100 ))
  P50=$(sed -n "${P50_LINE}p" "$TMP/sorted.txt")
  P95=$(sed -n "${P95_LINE}p" "$TMP/sorted.txt")
  P99=$(sed -n "${P99_LINE}p" "$TMP/sorted.txt")
else
  P50=0 P95=0 P99=0
fi

echo "duration_secs=$DUR total=$TOTAL success=$SUCCESS ratelimit=$RATELIMIT moderation_flagged=$MODERATION_HIT P50=${P50}ms P95=${P95}ms P99=${P99}ms"

# 注意：viewer 订阅链路本脚本未走 WS（PoC 阶段）；运维要观察 gateway
# /metrics 中 yunmao_gateway_subscribe_total / yunmao_chat_messages_in_total。
