# ADR-0010 投喂状态机：事件溯源 + 事务性出箱

- 状态：Accepted
- 日期：2026-05-24
- 关联：ADR-0003、ADR-0004（PostgreSQL）、03 章业务流程、04 章数据模型

## 决策

feeding-svc 把投喂状态机持久化到 PostgreSQL，采用：

1. **事件溯源**：每次状态变更（`created → accepted → queued → dispatched →
   acknowledged → succeeded/failed`）同事务追加一行
   `feeding_request_events`，主键 `(feed_request_id, seq)`。
2. **事务性出箱**：同事务再写一行 `outbox`，由独立的 outbox relay worker 顺序
   读取并投递到 Kafka，发布成功后标记 `published_at`。

本轮提供：

- 数据库迁移 `0002_outbox_and_events.sql`（`feeding_request_events`、`outbox`、
  `accounts`、`feeding_safety_policies`）。
- service 层 `EventListener` 接口与 `StateChangeEvent`，把状态变更与持久化
  解耦。生产实现将插入两张表。
- outbox relay worker 的位置占位（`pkg/yunmao/eventbus` 已具备所需的发布原语）。

> 本轮代码默认仍走 in-memory store；当 `YUNMAO_DB_URL` 注入后，下轮把 listener
> 替换为 PG 实现即可。

## 理由

- 业务上禁止“投喂指令发出但状态机没记录”或反之，事件溯源 + outbox 是经典解。
- 与决策 ADR-0003 的 at-least-once Kafka 语义天然匹配，消费端做幂等即可。
- `region_id` 字段已经在 `feed_requests`/`outbox` 预留，未来切 CRDB 不破坏表结构。

## 替代方案

- **双写（Kafka + DB）**：在分布式失败下会导致状态机不一致，弃。
- **Change Data Capture (Debezium)**：基础设施成本高，PoC 阶段不引入。
- **Saga**：投喂只有一条命令链路，不需要补偿框架的复杂度。

## 重新评估条件

- 业务出现长事务 / 多服务协调（开通付费/退款等）。
- outbox 表行数 > 1B，需要分区/归档策略。
