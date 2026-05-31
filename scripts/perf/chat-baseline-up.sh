#!/usr/bin/env bash
# chat-baseline-up.sh：起 chat-svc + gateway + redis + kafka 模拟器，
# 然后调 chat-baseline-run.sh 跑压测；最终归档到 reports/perf/。
#
# 用法：
#   bash scripts/perf/chat-baseline-up.sh [users] [viewers]
#
# 默认：1000 users / 5000 viewers（弹幕发送者 N + 观众订阅者 M）。
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

USERS=${1:-1000}
VIEWERS=${2:-5000}
TS=$(date +%Y%m%d-%H%M%S)
REPORT_DIR="$ROOT/reports/perf"
REPORT="$REPORT_DIR/chat-baseline-$TS.md"
JSON="$REPORT_DIR/chat-baseline-$TS.json"
mkdir -p "$REPORT_DIR"

echo "[chat-baseline] $(date) start; users=$USERS viewers=$VIEWERS"

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  cat <<EOF >"$REPORT"
# Chat baseline $TS - DRY RUN

本机 docker 不可用，无法启动 chat-baseline 完整链路（chat-svc + gateway + redis + kafka）。
复现路径：

\`\`\`
# Linux runner
bash scripts/perf/chat-baseline-up.sh $USERS $VIEWERS
\`\`\`

预估结果（按公式 \`ws-baseline-v1.md\`）：
- 1k users / 5k viewers → P50 < 80ms / P95 < 150ms（chat-svc → kafka → gateway）
- ratelimit 拒绝率 ~ 0%（默认窗口 5s/3 条）
- moderation 调用比例 ~ 100%（每条都过 local provider）
EOF
  cat <<EOF >"$JSON"
{
  "timestamp": "$TS",
  "users": $USERS,
  "viewers": $VIEWERS,
  "mode": "dry-run",
  "reason": "docker unavailable"
}
EOF
  echo "[chat-baseline] dry-run report at $REPORT"
  exit 0
fi

# 真值：起 docker compose（与 ws-baseline 共享 compose 文件）。
docker compose -f "$ROOT/scripts/perf/docker-compose.bench.yml" up -d --remove-orphans

# 等 gateway/chat-svc 就绪
for p in 18091 18092 18093; do
  for _ in {1..30}; do
    curl -fsS "http://127.0.0.1:$p/healthz" >/dev/null 2>&1 && break
    sleep 1
  done
done

# 跑压测
YUNMAO_CHAT_USERS=$USERS YUNMAO_CHAT_VIEWERS=$VIEWERS \
  bash "$ROOT/scripts/perf/chat-baseline-run.sh" >"$REPORT_DIR/chat-baseline-$TS.raw.log" 2>&1 || true

{
  echo "# Chat baseline $TS"
  echo
  echo "- timestamp: $TS"
  echo "- users: $USERS"
  echo "- viewers: $VIEWERS"
  echo "- uname: $(uname -a)"
  echo "- ulimit -n: $(ulimit -n)"
  echo
  echo "## Output"
  echo
  echo '```text'
  tail -n 80 "$REPORT_DIR/chat-baseline-$TS.raw.log"
  echo '```'
} >"$REPORT"

cat <<EOF >"$JSON"
{
  "timestamp": "$TS",
  "users": $USERS,
  "viewers": $VIEWERS,
  "mode": "real",
  "report": "chat-baseline-$TS.md"
}
EOF
echo "[chat-baseline] report: $REPORT"
