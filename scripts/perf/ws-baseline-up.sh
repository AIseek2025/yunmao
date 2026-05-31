#!/usr/bin/env bash
# ws-baseline-up.sh：拉起 WS 网关基线压测所需的完整环境：
#
#   - 3 个 gateway 实例（YUNMAO_GATEWAY_PORT=18091/18092/18093）
#   - 共享 redis（fanout）
#   - 共享 prometheus + grafana（已在 dev-up 中起好）
#   - feeding-svc memory 模式，仅作为事件生产者
#
# 真实 50k+ 连接基线请在 Linux x86 上跑（macOS sysctl + ulimit 上限较低）。
# 详见 scripts/perf/ws-baseline-report.md 与 ADR-0013。
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

if ! command -v cargo >/dev/null 2>&1; then
  echo "[perf] cargo not found, abort" >&2
  exit 1
fi

# 0. 起基础设施（postgres / redis / kafka / emqx / prom / grafana）
echo "[perf] start dev infra"
make dev-up

# 1. 编译 gateway release（多实例共享同一 binary）
echo "[perf] build gateway --release"
(cd rust && cargo build --release -p yunmao-gateway)

GATEWAY_BIN="$ROOT/rust/target/release/yunmao-gateway"

start_gateway() {
  local id=$1
  local port=$2
  local log=$3
  echo "[perf] start gateway-$id on :$port"
  # ADR-0019 第七轮起 HS256 路径已下线，gateway 走 RS256+JWKS。
  YUNMAO_GATEWAY_LISTEN="0.0.0.0:$port" \
    YUNMAO_GATEWAY_INSTANCE_ID="gw-$id" \
    YUNMAO_GATEWAY_REDIS_URL="redis://localhost:6379/0" \
    YUNMAO_JWT_ALG=RS256 \
    YUNMAO_JWKS_ENDPOINTS="${YUNMAO_JWKS_ENDPOINTS:-http://localhost:8101/jwks.json,http://localhost:8102/jwks.json}" \
    nohup "$GATEWAY_BIN" >"$log" 2>&1 &
  echo $! > "/tmp/yunmao-gw-$id.pid"
}

mkdir -p /tmp/yunmao-perf
start_gateway 1 18091 /tmp/yunmao-perf/gw1.log
start_gateway 2 18092 /tmp/yunmao-perf/gw2.log
start_gateway 3 18093 /tmp/yunmao-perf/gw3.log

# 2. 简单 health check
sleep 2
for p in 18091 18092 18093; do
  if curl -fsS "http://127.0.0.1:$p/healthz" >/dev/null; then
    echo "[perf] gw $p OK"
  else
    echo "[perf] gw $p NOT READY"
  fi
done

echo
echo "[perf] cluster ready. now run:  bash scripts/perf/ws-baseline-run.sh"
echo "[perf] tail logs:  tail -F /tmp/yunmao-perf/gw*.log"
