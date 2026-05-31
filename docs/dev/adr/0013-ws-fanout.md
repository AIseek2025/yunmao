# ADR-0013：WebSocket 网关跨实例扇出与限流策略

- 状态：accepted（第三轮迭代）
- 日期：2026-05-25
- 关联：
  - `rust/crates/yunmao-gateway/src/fanout.rs`
  - `rust/crates/yunmao-gateway/src/kafka_runtime.rs`
  - `rust/crates/yunmao-gateway/examples/bench_ws.rs`
  - `scripts/bench-ws.sh`

## 背景

第二轮迭代的 `yunmao-gateway` 使用 `tokio::sync::broadcast` + DashMap
维护房间→连接订阅，单实例可承载 5w～10w 长连。随着多实例水平扩展上线（每实例承
载约 2w 连接、3 实例预算），需要：

1. 任意一个实例 `publish` 房间事件时，**其它实例上的同房间订阅者**都能收到。
2. 单房间在线集合需要在跨实例可见（管理后台、计费、推荐位都会读）。
3. 优雅停机：实例下线时不能丢消息，要尽量让 client 切换到新连接前已订阅好对端。

## 决策

引入 `Fanout` trait 抽象，目前实现两种后端：

- `LocalFanout`（默认）：直接走 `Hub::broadcast`，兼容单实例模式，零依赖。
- `RedisFanout`：每个房间一个 Redis Pub/Sub channel（`yunmao:fanout:{room_id}`），
  并维护 `yunmao:room:{room_id}:online` SET。每个实例在启动时持有一个固定的
  `instance_id`，写入消息时把 `instance_id` 放进 envelope，订阅端自动过滤掉
  自家来的消息以避免双向回路。

并行做的另一件事是：

- 在 Kafka 模式（`YUNMAO_EVENT_BUS=kafka`）下，**保持 `kafka_runtime::spawn_fanout`
  只对本实例 hub 直接 broadcast**，不再额外通过 Redis publish。Kafka 已经把消息
  按 consumer-group 分发到了每个实例，再写一次 Redis 会引入 N² 倍重放。
- `publish_handler`（HTTP /publish 接口）以及 WS 内 chat 消息，统一走
  `state.fanout`，这是跨实例 fan-out 的关键入口。

## 替代方案

1. **直接用 Kafka 做 fan-out（每房间一个 topic）**：
   - 优点：复用已有 Kafka 基础设施。
   - 缺点：房间数动辄上千万；Kafka topic 数量上限和元数据压力太大；不支持 wildcard subscribe。
2. **NATS JetStream**：
   - 优点：原生支持 fan-out + 持久化。
   - 缺点：再多一个组件，第三轮没必要再引入。
3. **直接用 Redis Stream**：
   - 优点：有消费记录。
   - 缺点：长连 fan-out 不需要持久化（消息历史走 timeline 服务），Pub/Sub 更轻。

## 限流与并发保护

- 入站：每连接 token bucket（已实现于 `Hub`），单连接 chat 4 msg/s，超过则 drop+计数。
- 出站：单连接 outbound channel size = 1024；高背压会 close + log。
- 房间级：单房间 broadcast QPS 受 `state.fanout` 限频，未来通过 Redis Lua 滑动窗口
  实现房间 fan-out QPS 上限，第三轮先打 metric `gateway_publish_total`。
- 全局：每实例预算 5w 连接，超过则 503 `service_unavailable`。

## 基准

`rust/crates/yunmao-gateway/examples/bench_ws.rs` 提供原生 Rust tokio 压测客户端：

- 支持 `YUNMAO_BENCH_CONNS=50000 YUNMAO_BENCH_ROOMS=200`；
- 渐进 ramp up（默认 30s）防止 SYN flood；
- 每 5s 打印一次 connected / failed / msgs_in / subscribe_acks / errors；
- 退出时打印总数与时延。

`scripts/bench-ws.sh` 直接调用上述 example，无需额外安装 k6 / websocat。

### 本地实测样本（Apple M-series, 16 GB, macOS 25.5）

| 场景 | 实例数 | Redis | 连接 | 房间 | duration | connected | failed |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 单实例 in-memory | 1 | — | 5,000 | 20 | 30s | 5,000 | 0 |
| 单实例 in-memory | 1 | — | 20,000 | 100 | 60s | 20,000 | 0 |
| 双实例 + Redis | 2 | 7.x | 50,000 | 200 | 60s | 49,873 | 127 (\*) |

(\*) 失败均来自客户端 socket 数 `ulimit -n` 限制（macOS 默认 256）。在 Linux 容器
（`ulimit -n 200000`、`net.core.somaxconn=8192`、`net.ipv4.tcp_max_syn_backlog=8192`）
中复测 5w 全部建立成功。详见 `docs/dev/03-third-iteration-deliverable.md`。

## 迁移与开关

- 默认仍为 `LocalFanout`。设置 `YUNMAO_GATEWAY_REDIS_URL=redis://...` 后切到
  `RedisFanout`；`YUNMAO_GATEWAY_INSTANCE_ID` 不设则自动用 ULID。
- Redis 不可用时退化为 `LocalFanout`，并打印 warn 日志 + metric。
- 单实例 PoC 继续 work，无破坏性变更。
