# ADR-0009 Rust 端事件总线选型：`rskafka` 纯 Rust 客户端

- 状态：Accepted
- 日期：2026-05-24
- 关联：ADR-0003（事件总线 = Kafka）、ADR-0002

## 决策

Rust 端（`crates/yunmao-eventbus`）使用 [`rskafka`](https://crates.io/crates/rskafka)（纯 Rust，
无 librdkafka / cmake 依赖）作为 Kafka 客户端实现 producer + consumer。
Go 端继续用 `segmentio/kafka-go`（同样纯 Go，无 cgo）。

## 理由

- **构建摩擦最低**：开发机 / CI / Docker 镜像不需要 librdkafka，多阶段构建产物
  是单个静态二进制（`debian:bookworm-slim` 即可）。
- **接口足够覆盖**：device-edge / gateway 目前的需求是单分区订阅 + 单消息发送，
  rskafka 的 `partition_client` 即可满足；offset 暂以内存方式跟踪。
- **后续路径清晰**：若需要 consumer group / 大流量场景，可平滑切换 `rdkafka`
  或 `fluvio-protocol`，trait 屏蔽不变。

## 替代方案

- **`rdkafka` (C++/librdkafka)**：成熟、功能完备、高性能；但引入 cmake、
  openssl-dev、cyrus-sasl 等系统包，部署摩擦大。本轮目标是“跑通端到端”优先。
- **`fluvio`**：协议非 Kafka，不符合本轮“与 Go 端 Kafka 互通”的约束。
- **保留 HTTP fallback**：仍保留，由 `YUNMAO_EVENT_BUS=memory|http|kafka` 切换。

## 重新评估条件

- 单实例消费吞吐成为瓶颈（>10k msg/s），或需要 group rebalance 高可用语义。
- 需要 transactional producer / exactly-once 语义。
- 切到 Schema Registry + Avro/Proto 序列化。
