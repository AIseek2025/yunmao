#!/usr/bin/env bash
# 第八轮（D）：起 coturn 真值联调环境。
#
# 用法：
#   COTURN_EXTERNAL_IP=$(curl -s ifconfig.me) \
#     COTURN_STATIC_AUTH_SECRET=$(openssl rand -hex 32) \
#     bash scripts/turn/turn-up.sh
#
# 输出：环境变量值打印到 stdout，方便复制到 room-svc env。
# 若本机不在公网，留空 COTURN_EXTERNAL_IP 即可（仅 127.0.0.1 联调）。

set -euo pipefail

cd "$(dirname "$0")/../.."

: "${COTURN_EXTERNAL_IP:=127.0.0.1}"
: "${COTURN_STATIC_AUTH_SECRET:=$(openssl rand -hex 32)}"

export COTURN_EXTERNAL_IP COTURN_STATIC_AUTH_SECRET

echo "[turn-up] starting coturn (external-ip=$COTURN_EXTERNAL_IP)"
docker compose -f deploy/turn/docker-compose.turn.yml up -d --remove-orphans

echo
echo "[turn-up] coturn listening at:"
echo "  turn:$COTURN_EXTERNAL_IP:3478?transport=udp"
echo "  turns:$COTURN_EXTERNAL_IP:5349?transport=tcp"
echo "  static-auth-secret: $COTURN_STATIC_AUTH_SECRET"
echo
echo "[turn-up] export these to room-svc:"
echo "  YUNMAO_TURN_URL=turn:$COTURN_EXTERNAL_IP:3478?transport=udp"
echo "  YUNMAO_TURN_SECRET=$COTURN_STATIC_AUTH_SECRET"
echo
echo "[turn-up] sanity check (5s): docker compose ps + log tail"
sleep 2
docker compose -f deploy/turn/docker-compose.turn.yml ps
echo
docker compose -f deploy/turn/docker-compose.turn.yml logs --tail=30 coturn
echo
echo "[turn-up] verify relay (separate terminal): bash scripts/turn/turn-check.sh"
