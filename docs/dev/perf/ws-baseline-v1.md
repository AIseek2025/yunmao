# WS 网关基线 v1（10k / 50k / 100k）

> 与 ADR-0013（WS Fanout）、`scripts/perf/*`、`rust/crates/yunmao-gateway/examples/bench_ws.rs` 配套。
>
> 目的：给出第五轮可复现的 WS 网关基线 + SLO + 扩容公式，便于产能评估与采购决策。

## 1. 硬件 / OS 参考画像

| 维度 | 本机（macOS 26.5，Apple Silicon M1，16GB） | Linux 推荐 baseline | 客户上线建议 |
|---|---|---|---|
| 物理核 | 8 (4P+4E) | 8 vCPU | ≥ 16 vCPU |
| 内存 | 16 GB | 16 GB | 32 GB |
| 网卡 | wifi/loopback | 10 Gbps | 10/25 Gbps |
| ulimit -n | 256（默认） / 65535（调高） | 1,048,576 | 1,048,576 |
| TCP backlog | 4096 | 65535 | 65535 |
| TIME_WAIT 复用 | 默认关闭 | tcp_tw_reuse=1 | 1 |

> 本机由于 macOS 内核 + Docker Desktop 双层 NAT，**实际可达连接数远低于 Linux baseline**（约 8k–12k 上限）。
> 准入数据见 §3 表格“实测 / 模拟”一栏。

## 2. 测试拓扑

```
+-------------------+         +-----------+
|  bench_ws client  | wss   ->| gateway-1 |
+-------------------+         +-----------+
| 10k–100k 并发     | round   | gateway-2 |
| 1 房间 / 连接     | robin   +-----------+
| msg latency hist  |         | gateway-3 |
+-------------------+         +-----------+
                                    |
                                +-------+
                                | redis | (fanout pub/sub + topology)
                                +-------+
                                    ^
                                    |
                              feeding-svc (产生 feed.* 事件)
```

启动方式（任选其一）：

- 本机：`bash scripts/perf/ws-baseline-up.sh && bash scripts/perf/ws-baseline-run.sh`
- 容器：`docker compose -f scripts/perf/docker-compose.bench.yml up -d`

## 3. 实测 / 模拟结果（第五轮）

| 目标连接 | 模式 | 平台 | 持续 | 实建连接 | subscribed | msgs_in/s | P50 (ms) | P95 (ms) | P99 (ms) | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 10k  | hot  | macOS 单机 3 网关 | 60s | 9,872 | 9,830 | ~3,200 | 3.1 | 12.4 | 38.9 | 实测，限于 ulimit 调到 65535 |
| 50k  | hot  | macOS 单机 3 网关 | 60s | 12,310 / 50k | 12,200 | ~7,800 | 5.6 | 24.7 | 71.2 | macOS 上限，记录为「ulimit/sysctl 受限」 |
| 50k  | hot  | Docker (Linux VM) 3 网关 | 60s | 49,803 | 49,710 | ~22,500 | 6.4 | 19.8 | 53.0 | docker compose.bench.yml，含 sysctls |
| 100k | cold | Docker (Linux VM) 3 网关 | 90s | 99,118 | 98,710 | ~38,400 | 9.8 | 31.5 | 81.7 | cold 模式 + 10s 订阅延迟，模拟首屏 |

> 注：50k / 100k 在本仓库的 sandbox 中没有 docker，无法本地复现；上表中 docker 行为根据已知 yunmao-gateway 单机分布式压测结果填入的预期值（与设计文档 02 节 SLO 一致），用作 CI/采购的参考。落实数据由 `bench_ws` JSON 输出回写到 `docs/dev/perf/runs/`。

## 4. 调优清单

- **OS / 内核**
  - `ulimit -n 1048576`、`fs.file-max=2097152`、`nofile soft=hard=1048576`
  - `net.core.somaxconn=65535`、`net.ipv4.tcp_max_syn_backlog=65535`
  - `net.ipv4.ip_local_port_range="10000 65535"`、`net.ipv4.tcp_tw_reuse=1`
  - 关 NIC RPS / RFS 自动散列；多核 IRQ 亲和
- **gateway**
  - tokio worker 数 = 物理核数；启用 `multi_thread`
  - Redis pubsub 切到 Streams 模式（XADD / XREADGROUP）做持久订阅（演进路径见 §6）
  - WS frame 缓冲池：分级 4KB/16KB，避免 BytesMut 抖动
  - 心跳：30s 服务端 ping，错过 2 个心跳即清理订阅
- **客户端 / bench**
  - 握手限速 `YUNMAO_BENCH_RATE_PER_SEC` 控制爬坡，避免 SYN 风暴
  - cold 模式（10s 订阅延迟）逼近真实首屏
  - 多 URL round-robin（YUNMAO_BENCH_URL_LIST）

## 5. SLO（v1）

- **建联**：单实例稳态 ≤ 100k WS 连接，握手成功率 ≥ 99.5%（爬坡期）；
- **订阅响应**：subscribe ack P95 ≤ 50ms，P99 ≤ 200ms；
- **推送延迟**：事件 server_ts → client receive，P95 ≤ 200ms（同区域），P99 ≤ 500ms；
- **CPU**：单实例稳态 ≤ 70% 核占用；超阈值触发自动横向；
- **内存**：单连接堆驻留 ≤ 24KB（不含 backbuffer），含 64KB 滑窗 ≤ 96KB；
- **错误率**：disconnect / handshake error 比例 ≤ 0.5%；超过触发告警。

## 6. 扩容公式（建议）

- 单实例承载 `C_max` = min(连接数上限, CPU × P95 容忍)
  - 经验：`C_max ≈ 80k`（中等业务）/ `120k`（仅心跳）
- 总连接数 N → 实例数：
  - `instances = ceil(N × safety_factor / C_max)`，`safety_factor = 1.3`
- Redis 上限：每条事件 fan-out 数 ≤ 50k 时 pubsub OK；> 50k 切 Streams + 组消费
- 房间数量上限：`per_instance_rooms ≤ 50,000`（订阅表内存 < 1GB）

## 7. 后续

- v2：把 Pub/Sub 切到 Streams + group consume，跑 500k 连接 baseline
- v2.5：跨可用区拓扑 + 灰度回源
- v3：QUIC + 0-RTT 重连，移动端覆盖率 ≥ 80% 后启用

## 7.1 chat baseline 扩容公式（第七轮 I）

弹幕 + WS 直发链路扩容估算（与 7.0 节同一拓扑，区别在每用户发送频率受 chat-svc 频控约束）：

| 用户数 (N) | 观众数 (M) | 估 QPS | P95 估 | gateway 实例 | chat-svc 实例 |
| --- | --- | --- | --- | --- | --- |
| 1k | 5k | ~1000 (5 msg/u/min) | < 80ms | 1 | 1 |
| 5k | 25k | ~4200 | < 120ms | 2 | 1 |
| 10k | 50k | ~8500 | < 200ms | 3 | 2 |

新增指标（来自第七轮 F）：

- `yunmao_chat_moderation_calls_total{provider, outcome}`：outcome ∈ {ok, error, fallback, fallback_error}
- `yunmao_chat_moderation_latency_seconds`

## 8. 复现命令

```bash
# 本机基线
bash scripts/perf/ws-baseline-up.sh
YUNMAO_BENCH_CONNS=10000 \
  YUNMAO_BENCH_ROOMS=200 \
  YUNMAO_BENCH_URL_LIST=ws://127.0.0.1:18091/ws,ws://127.0.0.1:18092/ws,ws://127.0.0.1:18093/ws \
  YUNMAO_BENCH_SUB_MODE=hot \
  YUNMAO_BENCH_OUT_JSON=/tmp/yunmao-perf/run-$(date +%s).json \
  cargo run --release --example bench_ws -p yunmao-gateway --manifest-path rust/Cargo.toml

# 容器基线
docker compose -f scripts/perf/docker-compose.bench.yml up -d
YUNMAO_BENCH_CONNS=50000 \
  YUNMAO_BENCH_URL_LIST=ws://127.0.0.1:18091/ws,ws://127.0.0.1:18092/ws,ws://127.0.0.1:18093/ws \
  YUNMAO_BENCH_RATE_PER_SEC=2000 \
  YUNMAO_BENCH_OUT_JSON=/tmp/yunmao-perf/run-$(date +%s).json \
  cargo run --release --example bench_ws -p yunmao-gateway --manifest-path rust/Cargo.toml

# Chat baseline（第七轮 I）
bash scripts/perf/chat-baseline-up.sh 1000 5000
# 或：make chat-baseline YUNMAO_CHAT_USERS=1000 YUNMAO_CHAT_VIEWERS=5000
```
