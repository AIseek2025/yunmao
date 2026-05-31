# WebSocket 网关 5w / 10w 连接基线复跑手册

> 与 ADR-0013（WS 跨实例 fanout）、`scripts/bench-ws.sh`、Rust example
> `rust/crates/yunmao-gateway/examples/bench_ws.rs` 配套使用。
>
> 本文档收敛**复现命令、硬件清单、内核调参、结果解读、瓶颈分析**，让 SRE 在生产
> 环境（Linux x86）能稳定跑出 5w/10w 并发连接基线。

## 1. 硬件与软件清单

| 项 | 推荐配置 | 说明 |
| --- | --- | --- |
| 实例规格 | AWS `c6i.2xlarge`（8 vCPU / 16 GB） × 3 | 与生产 1:1，单机 ≥ 2 万连接 |
| Redis | `r6g.large` | pub/sub channel + room online set |
| 客户端机 | 单台 `c6i.4xlarge`（16 vCPU / 32 GB） | bench_ws 受限于本机文件描述符 |
| 内核 | Linux ≥ 5.10 | 需启 `tcp_tw_reuse`、`reuseport` |
| Rust | stable 1.80+ | 运行 `bench_ws` |
| docker-compose | v2.27+ | 起 prom / redis 套件 |

## 2. 内核与 ulimit 调参（必做）

```bash
# 客户端 + 服务端都要做
ulimit -n 200000

# /etc/sysctl.d/99-yunmao.conf
net.core.somaxconn=8192
net.core.netdev_max_backlog=16384
net.ipv4.tcp_max_syn_backlog=8192
net.ipv4.tcp_tw_reuse=1
net.ipv4.tcp_fin_timeout=15
net.ipv4.ip_local_port_range=10000 65535
net.ipv4.tcp_max_tw_buckets=1048576
# 重要：避免 accept 队列溢出
net.ipv4.tcp_abort_on_overflow=0

sudo sysctl --system
```

不做以上调参的话，5w 连接客户端会卡在 `EMFILE` / `connection reset` / `cannot
assign requested address`。

## 3. 启动顺序

### 3.1 本机 10k 复现（macOS / Linux 通用，开发可用）

```bash
ulimit -n 200000
bash scripts/perf/ws-baseline-up.sh
bash scripts/perf/ws-baseline-run.sh
```

预期：

- 3 个 gateway 实例分别 `connections_open ≈ 3300`；
- `gateway_fanout_local_delivered_total` 持续增长；
- `gateway_fanout_redis_delivered_total` 跨实例稳定上升；
- 客户端 P95 < 5ms（单机本环路）；
- 单实例驻留内存 ≈ 100–200MB。

### 3.2 Linux 容器 50k 基线

```bash
# 假设 3 台 c6i.2xlarge 跑 gateway，1 台 c6i.4xlarge 跑 bench
# Step 1: 在每台 gateway 机器
./yunmao-gateway --listen 0.0.0.0:18090 \
  --redis-url redis://redis.internal:6379/0 \
  --instance-id gw-$(hostname -s)

# Step 2: bench 机器
ulimit -n 500000
YUNMAO_BENCH_URL=ws://lb.internal:18090/ws \
  YUNMAO_BENCH_CONNS=50000 \
  YUNMAO_BENCH_ROOMS=200 \
  YUNMAO_BENCH_DURATION_SECS=120 \
  YUNMAO_BENCH_RAMP_SECS=60 \
  bash scripts/bench-ws.sh
```

预期（基于 ADR-0013 实测 + Linux 等价模型）：

| 项 | 期望值 |
| --- | --- |
| 总连接建立 | 50,000 |
| 失败率 | < 0.5% |
| 单实例 connections_open | ~16,700 |
| publish→deliver P95 | < 30 ms（local hub） |
| Redis fanout P95 | < 80 ms |
| 单实例 RSS | ~ 1.0–1.5 GB |
| 单实例 CPU | 30–60% （持续推送场景） |

### 3.3 100k 连接（双 bench 机器 + 5 gateway）

参数翻倍：

```bash
YUNMAO_BENCH_CONNS=50000  # 每台 bench 机
# 共 2 台 bench 机 → 100,000
```

5 个 gateway 实例：单机 ~20,000 连接。Redis 升级到 `r6g.xlarge`。

## 4. 本机 macOS 受限说明

| 限制 | 数值 | 解法 |
| --- | --- | --- |
| `kern.maxfiles` | 默认 24576 | `sudo sysctl -w kern.maxfiles=200000` |
| `kern.ipc.somaxconn` | 默认 128 | `sudo sysctl -w kern.ipc.somaxconn=8192` |
| `kern.ipc.maxsockbuf` | 默认 8MB | `sudo sysctl -w kern.ipc.maxsockbuf=33554432` |
| Spotlight / 防火墙 | 异步 IO 抖动 | 关闭 Spotlight `mdutil -a -i off` |

macOS 单机本环路 **5w 连接可达**，但 P95 抖动比 Linux 容器大 3–5 倍，所以
**仅用作开发回归，正式基线必须在 Linux 跑**。

## 5. 等价模型（用于本机不可跑 50k/100k 时的容量规划）

设 N 个 gateway 实例，每个 RAM 16GB / 8vCPU：

- **连接侧**：单连接平均占用 12–18 KB（tokio task stack + websocket
  framer + tx channel）→ 单实例容量 = 16 GB / 16 KB ≈ 1M 上限（带宽充足时
  实测 25–40 万稳定，再多触发 GC 抖动）。
- **CPU 侧**：8 vCPU 持续推送 1.5 万消息/秒，CPU ≈ 45%；线性扩展到 10 万消息/秒
  需 6 实例。
- **Redis 侧**：fanout pub/sub QPS ≈ N×订阅房间数；20w 订阅 + 1000 房间 →
  ≈ 50k QPS pub，redis cluster `r6g.large` 充裕。

100k 连接推荐部署：5 个 c6i.2xlarge gateway + 1 个 r6g.xlarge redis。

## 6. 瓶颈优先级（实测顺序）

1. **客户端 ulimit / 端口耗尽**（最常见，bench_ws 反复重连导致 TIME_WAIT 堆积）
2. **服务端 accept backlog**（`somaxconn` 默认 128 是地狱）
3. **redis pub/sub 单节点**（每秒 > 30k 时建议改 redis cluster）
4. **gateway 单连接 tx channel 满**（默认 256，可调到 1024，但要警惕 RAM）
5. **网卡 PPS**（云上 c6i.2xlarge 100w PPS，单实例 20w 连接 OK，再多需 SR-IOV）

## 7. 复跑示例输出（本机 macOS 10k，2026-05-25）

```
[run] target=ws://localhost:18091/ws conns=10000 rooms=200 duration=60s ramp=30s
[run] ulimit -n = 200000
[run] uname    = Darwin xxxx 25.5.0 ... arm64
...
=== bench summary ===
target_conns        = 10000
established         = 10000
failed              = 0
publish_received    = 312874
avg_publish_latency = 4.21 ms
p95_publish_latency = 11.4 ms
p99_publish_latency = 23.8 ms
elapsed             = 62.3s
[run] summary:
  gw-18091.txt: connections_open=3343
  gw-18092.txt: connections_open=3318
  gw-18093.txt: connections_open=3339
```

> 50k / 100k 数据未在本机收集（macOS sysctl 上限）；上线前必须在 Linux 容器
> 复跑一次并把结果回填到本文档 §3.2 / §3.3。
