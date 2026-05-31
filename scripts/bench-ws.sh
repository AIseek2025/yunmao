#!/usr/bin/env bash
# WebSocket 网关基准压测（依托 rust crate yunmao-gateway/examples/bench_ws.rs）。
#
# 用法：
#   YUNMAO_BENCH_URL=ws://localhost:8090/ws \
#   YUNMAO_BENCH_CONNS=50000 \
#   YUNMAO_BENCH_ROOMS=200 \
#   YUNMAO_BENCH_DURATION_SECS=60 \
#   YUNMAO_BENCH_RAMP_SECS=30 \
#   ./scripts/bench-ws.sh
#
# 注意：
# - macOS / Linux 下需先放开 `ulimit -n` 至 100k+（CONNS > 1024 必做）。
# - 推荐 5w 连接：`ulimit -n 200000` 并使用 docker-compose 起 2-3 个 gateway + 1 个 redis。
# - 单机网卡/TCP backlog 是常见瓶颈；详见 docs/dev/adr/0013-ws-fanout.md。

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT/rust"

URL=${YUNMAO_BENCH_URL:-ws://localhost:8090/ws}
CONNS=${YUNMAO_BENCH_CONNS:-5000}
ROOMS=${YUNMAO_BENCH_ROOMS:-20}
DURATION=${YUNMAO_BENCH_DURATION_SECS:-60}
RAMP=${YUNMAO_BENCH_RAMP_SECS:-30}

echo "[bench-ws] target=$URL conns=$CONNS rooms=$ROOMS duration=${DURATION}s ramp=${RAMP}s"
echo "[bench-ws] $(uname -a)"
echo "[bench-ws] file descriptors: $(ulimit -n)"

YUNMAO_BENCH_URL=$URL \
  YUNMAO_BENCH_CONNS=$CONNS \
  YUNMAO_BENCH_ROOMS=$ROOMS \
  YUNMAO_BENCH_DURATION_SECS=$DURATION \
  YUNMAO_BENCH_RAMP_SECS=$RAMP \
  cargo run --release --example bench_ws -p yunmao-gateway
