# ADR-0003 事件总线选型

- 状态：Proposed（架构负责人建议）
- 日期：2026-05-24
- 关联：`01-技术选型与系统架构.md`、`04-设备接入数据模型与API边界.md`

## 决策

推荐以 **Kafka + CloudEvents 信封** 作为 yunmao 的默认事件总线。RocketMQ 保留为中国云厂商托管环境或团队强经验场景下的备选。

## 理由

- Kafka 与 CloudEvents、Schema Registry、ClickHouse、Flink、CDC、离线分析链路的生态组合更成熟。
- 投喂、设备、QoE、计费、运营事件都需要重放、削峰和多消费者订阅，Kafka 的通用性更稳。
- 招聘、排障资料、SRE 经验和本地 docker-compose 验证成本更可控。

## 替代方案

- RocketMQ：国内云厂商托管成熟，事务消息和延迟消息能力直接，但跨语言生态与分析生态不如 Kafka 普遍。
- NATS JetStream：轻量低延迟，但大数据与长期事件治理生态较弱。
- 纯 gRPC 同步：初期简单，但无法承担削峰、重放和异步解耦。

## 重新评估条件

- 团队已有 RocketMQ 运维经验，且目标云厂商托管 RocketMQ 的成本、稳定性、SLA 明显优于 Kafka。
- 事件模型被证明主要是低延迟命令而非可重放事件流。
- Kafka 运维成本在 MVP 阶段成为明确阻塞。
