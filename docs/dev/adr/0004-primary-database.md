# ADR-0004 主数据库选型

- 状态：Proposed（架构负责人建议）
- 日期：2026-05-24
- 关联：`01-技术选型与系统架构.md`、`04-设备接入数据模型与API边界.md`

## 决策

MVP 推荐 **PostgreSQL 起步**，不直接引入 CockroachDB。数据模型从第一天保留 `region_id`、业务分片键、读写分离和多活迁移口。

## 理由

- MVP 目标是单 region、多 AZ 可用性与快速业务验证，PostgreSQL 足以承载用户、房间、设备、投喂、订单等核心数据。
- CockroachDB 会提前带来分布式事务延迟、SQL 兼容性、索引设计、运维和故障排查复杂度。
- 通过区域字段、ID 前缀、分片键和事件总线出站表，可以为后续分布式 SQL 或跨 region 迁移留足空间。

## 替代方案

- CockroachDB：适合多 region 强一致写入，但 MVP 成本过高。
- MySQL：生态成熟，团队若更熟悉也可用；当前 docker-compose 与迁移已偏向 PostgreSQL。
- TiDB：适合 HTAP 或大规模分布式 SQL，但早期运维复杂度不低。

## 重新评估条件

- Phase 2 前出现真实多 region 强一致写入需求，例如跨区域订单/权益必须同步提交。
- PostgreSQL 分库分表或读写分离已无法满足增长期容量。
- 团队具备分布式 SQL 的稳定运维能力，并已完成故障演练。
