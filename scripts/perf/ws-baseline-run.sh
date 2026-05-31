#!/usr/bin/env bash
# ws-baseline-run.sh：对 ws-baseline-up.sh 拉起的 3 实例 gateway 跑压测。
#
# 可改环境变量：
#   YUNMAO_BENCH_URL_LIST   逗号分隔多实例 URL（默认 3 实例 round-robin）
#   YUNMAO_BENCH_CONNS      目标连接数（默认 10000）
#   YUNMAO_BENCH_ROOMS      订阅房间数（默认 200）
#   YUNMAO_BENCH_DURATION   持续秒（默认 60）
#
# 输出：
#   /tmp/yunmao-perf/run-<ts>/{stats.json, prom.txt}
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

URL=${YUNMAO_BENCH_URL:-ws://localhost:18091/ws}
CONNS=${YUNMAO_BENCH_CONNS:-10000}
ROOMS=${YUNMAO_BENCH_ROOMS:-200}
DURATION=${YUNMAO_BENCH_DURATION_SECS:-60}
RAMP=${YUNMAO_BENCH_RAMP_SECS:-30}

TS=$(date +%Y%m%d-%H%M%S)
OUT="/tmp/yunmao-perf/run-$TS"
mkdir -p "$OUT"

echo "[run] target=$URL conns=$CONNS rooms=$ROOMS duration=${DURATION}s ramp=${RAMP}s"
echo "[run] ulimit -n = $(ulimit -n)"
echo "[run] uname    = $(uname -a)"
echo "[run] out      = $OUT"

YUNMAO_BENCH_URL=$URL \
  YUNMAO_BENCH_CONNS=$CONNS \
  YUNMAO_BENCH_ROOMS=$ROOMS \
  YUNMAO_BENCH_DURATION_SECS=$DURATION \
  YUNMAO_BENCH_RAMP_SECS=$RAMP \
  cargo run --release --example bench_ws -p yunmao-gateway --manifest-path "$ROOT/rust/Cargo.toml" \
  | tee "$OUT/bench.log"

# 抓 prometheus snapshot
for p in 18091 18092 18093; do
  if curl -fsS "http://127.0.0.1:$p/metrics" -o "$OUT/gw-$p.txt"; then
    echo "[run] saved gw $p metrics"
  fi
done

# 汇总：connections_open
echo
echo "[run] summary:"
for f in "$OUT"/gw-*.txt; do
  open=$(awk '/^gateway_connections_open / {print $2}' "$f" 2>/dev/null | head -n1)
  echo "  $(basename "$f"): connections_open=${open:-?}"
done

echo "[run] log dir: $OUT"
