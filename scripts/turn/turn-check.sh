#!/usr/bin/env bash
# 第八轮（D）：用 coturn 自带 turnutils_uclient 验证 relay 工作。
#
# 前置：scripts/turn/turn-up.sh 已起；
#       export COTURN_EXTERNAL_IP=... COTURN_STATIC_AUTH_SECRET=...
#
# 输出：relay socket 创建成功 → RTT / 包率统计。

set -euo pipefail

: "${COTURN_EXTERNAL_IP:?need COTURN_EXTERNAL_IP}"
: "${COTURN_STATIC_AUTH_SECRET:?need COTURN_STATIC_AUTH_SECRET}"
: "${TURN_TEST_COUNT:=20}"
: "${TURN_TEST_RATE_KBPS:=100}"

cd "$(dirname "$0")/../.."

# 用 RFC7635 timestamp:rand 形式签发短期凭证（与 room-svc Signer 同构）
expiry=$(( $(date +%s) + 600 ))
username="${expiry}:test_user"
password=$(
  printf '%s' "$username" \
    | openssl dgst -sha1 -hmac "$COTURN_STATIC_AUTH_SECRET" -binary \
    | base64
)

echo "[turn-check] username=$username"
echo "[turn-check] password=$password"
echo "[turn-check] running turnutils_uclient against $COTURN_EXTERNAL_IP:3478 ..."

# 默认 5 秒压测；可改：TURN_TEST_COUNT=100
docker run --rm -it --network=host instrumentisto/coturn:4.6 \
  turnutils_uclient \
    -u "$username" -w "$password" \
    -y -n "$TURN_TEST_COUNT" -m 1 -l 64 -r "$TURN_TEST_RATE_KBPS" \
    "$COTURN_EXTERNAL_IP" 3478 \
  || { echo "[turn-check] FAILED"; exit 1; }

echo "[turn-check] OK"
