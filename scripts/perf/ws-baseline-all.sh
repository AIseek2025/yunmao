#!/usr/bin/env bash
# ws-baseline-all.sh：一键流程
#
#   1. 跑 docker-compose.bench.yml（基础设施 + 3 gateway）
#   2. 等 gateway 就绪
#   3. 跑 bench_ws（来源 scripts/perf/ws-baseline-run.sh）
#   4. 把结果归档到 reports/perf/ws-baseline-<date>.md
#
# 用法：
#   bash scripts/perf/ws-baseline-all.sh [conns] [duration_secs]
#
# 默认：10000 conns / 60s。Linux 大机器可改 50000/300。
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

CONNS=${1:-10000}
DURATION=${2:-60}
TS=$(date +%Y%m%d-%H%M%S)
REPORT_DIR="$ROOT/reports/perf"
REPORT="$REPORT_DIR/ws-baseline-$TS.md"
mkdir -p "$REPORT_DIR"

echo "[perf-all] $(date) start, conns=$CONNS, duration=${DURATION}s"

# 优先用 docker compose；若没 docker，提示并退出。
if ! command -v docker >/dev/null 2>&1; then
  cat <<EOF >"$REPORT"
# ws baseline $TS - DRY RUN (docker unavailable)

本机不存在 \`docker\` 二进制，无法跑 docker-compose.bench.yml。建议在 Linux self-hosted runner 上跑。
EOF
  echo "[perf-all] no docker, dry-run report at $REPORT"
  exit 0
fi

if ! docker info >/dev/null 2>&1; then
  cat <<EOF >"$REPORT"
# ws baseline $TS - DRY RUN (docker daemon unavailable)

\`docker info\` 失败；请先启动 Docker Desktop / Docker Engine 后重跑。
EOF
  echo "[perf-all] no docker daemon, dry-run report at $REPORT"
  exit 0
fi

echo "[perf-all] compose up"
docker compose -f "$ROOT/scripts/perf/docker-compose.bench.yml" up -d --remove-orphans

# 等 gateway 就绪
for p in 18091 18092 18093; do
  for i in {1..30}; do
    if curl -fsS "http://127.0.0.1:$p/healthz" >/dev/null 2>&1; then
      echo "[perf-all] gw $p ready"
      break
    fi
    sleep 1
  done
done

# 跑 bench_ws（沿用 ws-baseline-run.sh）
YUNMAO_BENCH_CONNS=$CONNS YUNMAO_BENCH_DURATION_SECS=$DURATION \
  bash "$ROOT/scripts/perf/ws-baseline-run.sh" || true

LATEST=$(ls -dt /tmp/yunmao-perf/run-* 2>/dev/null | head -n1 || true)
JSON="$REPORT_DIR/ws-baseline-$TS.json"
{
  echo "{"
  echo "  \"timestamp\": \"$TS\","
  echo "  \"conns\": $CONNS,"
  echo "  \"duration_secs\": $DURATION,"
  echo "  \"uname\": \"$(uname -a | sed 's/"/\\"/g')\","
  echo "  \"ulimit_n\": \"$(ulimit -n)\","
  echo "  \"report\": \"ws-baseline-$TS.md\""
  echo "}"
} >"$JSON"
{
  echo "# WS baseline run $TS"
  echo
  echo "- timestamp: $TS"
  echo "- conns: $CONNS"
  echo "- duration: ${DURATION}s"
  echo "- uname: $(uname -a)"
  echo "- ulimit -n: $(ulimit -n)"
  echo
  if [[ -n "$LATEST" && -f "$LATEST/bench.log" ]]; then
    echo "## bench_ws output"
    echo
    echo '```text'
    tail -n 80 "$LATEST/bench.log"
    echo '```'
    echo
    echo "## Prometheus snapshots"
    for f in "$LATEST"/gw-*.txt; do
      [[ -e "$f" ]] || continue
      echo
      echo "### $(basename "$f")"
      echo '```'
      grep -E '^gateway_(connections_open|publish_total|chat_in_total|subscribe_total) ' "$f" || true
      echo '```'
    done
  fi
} >"$REPORT"

echo "[perf-all] report: $REPORT"
