# 第二轮交付物（核心底层模块·Kafka/PG/Redis/JWT/Buf/LL-HLS-stub/web-demo）

> 上一轮：见 `01-bootstrap-deliverable.md`。
> 本轮架构决策见 `adr/0009 – 0011` 以及 `07-决策记录与待确认问题.md`。

本轮把上一轮“PoC 链路（RTMP→HTTP-FLV、WS 订阅、内存投喂状态机）”升级为
“可真正接 Kafka / PostgreSQL / Redis 的底座”。

## 关键能力一句话清单

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| Kafka 事件总线（Go + Rust）| ✅ | CloudEvents 1.0 信封；Go=`segmentio/kafka-go`、Rust=`rskafka`；`feed.command.*` / `device.state.*` / `live.stream.*` 全覆盖 |
| feeding-svc Kafka 路径 | ✅ | `YUNMAO_EVENT_BUS=memory|http|kafka` 三档；publisher 抽象统一 |
| device-edge Kafka 路径 | ✅ | 订阅 `feed.command.requested`、Kafka 发布 `feed.command.acked` |
| gateway Kafka 扇出 | ✅ | 订阅 `feed.* / device.state.* / live.stream.*`，按 `room_id` 广播到 WS |
| PostgreSQL 接入 | 🟡 | 迁移文件 + `pgxpool` + 简易 migration runner；feeding-svc 仍内存 store，listener 钩子已就位 |
| Outbox + 事件溯源 | 🟡 | 表结构 + service listener 接口；relay worker 留作下轮 |
| Redis 接入（Go + Rust） | ✅ | Go：冷却/幂等/限流；Rust：`yunmao-redis` crate（`SETNX EX` 幂等）|
| 投喂安全参数配置化 | ✅ | `feedingsafety.Manager`：默认 → env → admin store；`admin-svc` 暴露 `GET/PUT /v1/admin/feeding-safety` |
| WebSocket JWT 鉴权 | ✅ | user-svc 签发 login JWT；room-svc 签发短期 room subscription token；gateway `ClientFrame::Auth` 校验签名 + room 字段 |
| Buf / Proto 工具链 | ✅ | `proto/buf.yaml` + `buf.gen.yaml`，`scripts/gen-proto.sh` 走 `buf lint/build/generate`；feeding-svc 暴露 gRPC，device-svc 演示客户端调用 |
| LL-HLS 接口预留 | ✅ | `mod ll_hls` + 501 路由 + Prom 计数 |
| 可观测性 | ✅ | Go：`yunmao_eventbus_*`、`yunmao_feed_*`、`yunmao_feed_cooldown_*`；Rust：`gateway_*`、`media_edge_*`、`media_edge_ll_hls_*` |
| clients/web-demo | ✅ | 静态 HTML + flv.js + WS + 登录 + 投喂按钮 |
| Docker Compose 应用层 | ✅ | `deploy/docker-compose.app.yml`（含 6 个 Go svc + 3 个 Rust 二进制） |
| Makefile 升级 | ✅ | `app-up/down`、`web-demo`、`buf-{lint,breaking,generate}`、`migrate-up` |

🟡 表示底座具备，但生产级落地（如真正从 PG 读取写入）留到下一轮替换实现。

## 新增 / 修改文件

### 文档与 ADR

- 新增：`docs/dev/adr/0009-rust-eventbus-rskafka.md`
- 新增：`docs/dev/adr/0010-outbox-and-event-sourcing.md`
- 新增：`docs/dev/adr/0011-ll-hls-interface-reserved.md`
- 新增：`docs/dev/02-second-iteration-deliverable.md`（本文档）

### Go 平台共享包

- 新增：`go/pkg/yunmao/eventbus/{eventbus.go, kafka.go, eventbus_test.go, metrics.go}`
- 新增：`go/pkg/yunmao/db/{pg.go, pg_test.go}`
- 新增：`go/pkg/yunmao/cache/{cache.go, cooldown.go, idempotent.go, ratelimit.go, cache_test.go}`
- 新增：`go/pkg/yunmao/authjwt/{jwt.go, jwt_test.go}`
- 新增：`go/pkg/yunmao/feedingsafety/{feedingsafety.go, feedingsafety_test.go}`

### Go service

- feeding-svc：`internal/service/{service.go, service_test.go, metrics.go}`、
  `internal/publisher/{publisher.go, http_publisher.go, kafka_publisher.go}`、
  `internal/transport/{http.go, grpc.go}`、`cmd/feeding-svc/main.go`
- user-svc：`internal/service/service.go`（DevLogin、JWKS）、
  `internal/transport/http.go`、`cmd/user-svc/main.go`
- room-svc：`internal/service/service.go`（IssueSubscription）、
  `internal/transport/http.go`、`cmd/room-svc/main.go`
- admin-svc：`internal/service/service.go`（GlobalSafety）、
  `internal/transport/http.go`、`cmd/admin-svc/main.go`
- device-svc：`internal/transport/http.go`（gRPC client demo）、
  `cmd/device-svc/main.go`、`go.mod`

### Go 迁移与 proto

- 修改：`go/migrations/0001_init.sql`（`region_id` / `updated_at` / `feed_cooldown_seconds=30`）
- 新增：`go/migrations/0002_outbox_and_events.sql`
- 新增：`go/proto/go.mod`（Buf 生成的 Go stub 独立模块）
- 修改：`go/go.work`、各 service `go.mod`（指向本地 `proto` 模块）

### Rust crates

- 新增：`rust/crates/yunmao-eventbus/{Cargo.toml, src/lib.rs, src/topic.rs,
  src/memory.rs, src/kafka.rs}`
- 新增：`rust/crates/yunmao-redis/{Cargo.toml, src/lib.rs}`
- device-edge：`src/{kafka_runtime.rs, server.rs, main.rs, lib.rs}`
- gateway：`src/{auth.rs, kafka_runtime.rs, server.rs, main.rs, lib.rs}`、
  `Cargo.toml`（新增 jsonwebtoken / yunmao-eventbus）
- media-edge：`src/{ll_hls.rs, lib.rs, server.rs, metrics.rs}`
- protocol：`src/signaling.rs`（新增 `ClientFrame::Auth`）

### proto 工具链

- 新增：`proto/buf.yaml`、`proto/buf.gen.yaml`
- 修改：`scripts/gen-proto.sh`（改用 buf）

### 客户端 Demo

- 新增：`clients/web-demo/{index.html, demo.js, styles.css, README.md}`

### Deploy & 工具

- 新增：`deploy/docker-compose.app.yml`
- 新增：`go/Dockerfile`、`rust/Dockerfile`（多阶段构建）
- 修改：`deploy/observability/grafana/dashboards/yunmao-overview.json`（新增 6 个面板）
- 修改：`Makefile`（`app-up/down`、`web-demo`、`buf-*`、`migrate-up/down`）

## 模块能力 + 单测状态

- `pkg/yunmao/eventbus`：`MemoryBus`（单测覆盖发布/订阅/dispatch retry），
  `KafkaBus`（基于 `segmentio/kafka-go`，需本地 Kafka 才能联调，单测只覆盖 envelope）。✅
- `pkg/yunmao/db`：`Open`+`Migrate`+`LoadMigrationsFS`，迁移文件解析单测通过。✅
- `pkg/yunmao/cache`：`MemoryStore`/`RedisStore`、`Cooldown`/`Idempotent`/`Ratelimit`，
  冷却 + 每日上限测试通过。✅
- `pkg/yunmao/authjwt`：HS256 签发 / 校验 / JWKS 占位，过期 + 签名错误覆盖。✅
- `pkg/yunmao/feedingsafety`：默认值 + env 覆盖 + store 覆盖优先级测试。✅
- feeding-svc：内存路径 + Kafka publisher（go vet/test 通过）。✅
- user-svc / room-svc / admin-svc：JWT 链路 + 安全参数 + 房间订阅 token 测试。✅
- Rust `yunmao-eventbus`：`MemoryBus` 单测；`KafkaBus` 需要本地 Kafka 集成测。✅
- Rust `yunmao-redis`：`MemoryCache` + `IdempotentStore` 单测。✅
- gateway auth：HS256 token 校验 / room scope / 拒绝错密钥单测。✅
- media-edge `ll_hls`：占位 packager 单测。✅

## 真正跑通的验证命令

```bash
# Go
make go-vet                                # 所有模块绿
make go-test                               # 所有模块绿（含新 eventbus / cache / authjwt / feedingsafety）

# Rust
cd rust && cargo fmt --all -- --check
cd rust && cargo clippy --workspace --all-targets -- -D warnings   # ✅ 0 warning
cd rust && cargo test --workspace          # ✅ 全绿

# Proto
make buf-lint                              # ✅
make gen-proto                             # ✅（产物在 go/proto/）

# Kafka 端到端（需要 docker compose）
make dev-up                                # 起 kafka / postgres / redis / jaeger / prometheus / grafana
make app-up                                # 起 6 个 Go svc + 3 个 Rust 二进制
make web-demo                              # 浏览器打开 http://localhost:5173/
# 在 demo 页：登录 → 取房间订阅 token → 连 WS → 订阅 room_demo → 投喂；
# 应看到 feed.command.dispatched / feed.command.acked 事件按顺序到达。
```

## 仍未做完的硬核 TODO

1. **MQTT 真实接入**：device-svc → 实体设备的下行通道仍是 mock；本轮聚焦事件总线。
2. **ABR 转码 / WHEP**：media-edge 仅 HTTP-FLV，未引入 ffmpeg/x264/SVT-AV1 与 WebRTC。
3. **LL-HLS 切片器实现**：本轮只有 trait + 501 占位。
4. **WebRTC** 全链路（信令、SFU、回退）整体推迟。
5. **5 万并发 WS 压测**：仅 `make bench-ws` 单进程压测脚本，没有跨机房 / 多实例。
6. **KMS / Vault**：JWT secret 与设备凭证仍走 env；ADR-0007 已留口。
7. **CRDB 迁移路径**：`region_id` 字段已加，未做读写分离 / 多 region 测试。
8. **sqlc 真正接入**：本轮只补 schema；query 文件下一轮统一生成。
9. **outbox relay worker**：events 表结构已建，未启动 worker。
10. **gateway 在线集合 / 用量统计**：Hub 内存维护，未接 Redis。

## 本轮新增的临时决策

- Rust 端 Kafka 选 `rskafka` 而非 `rdkafka`：纯 Rust 优先，避免 librdkafka 引入
  额外系统依赖；接口足够 device-edge / gateway 当前需求。详见 ADR-0009。
- Rust gateway JWT 校验当前不强校验 issuer（user-svc 与 room-svc 共用密钥但 issuer
  不同）。需要在 RS256 切换时与 JWKS 一并整改。
- 投喂安全参数现存于内存 `MemoryStore`；admin 写入只在单实例可见，多副本需切 PG/Redis。

## 仍需用户 / 业务确认的问题

1. 投喂上限单位最终采用 **g** 还是 **次**？现在按 12g/天 落，但 admin UI 文案需确认。
2. RTMP 推流密钥的发放与吊销流程（device-svc 现在 mock）；当前是否需要走管理后台？
3. 灰度发布是否需要按 region 拆 broker（同地域多副本 vs 跨区主备）？

## 下一轮建议优先级

1. **outbox relay + PG-backed feeding-svc**：把 listener 钩子接到 PG，启动
   relay 把 `outbox` 行投递到 Kafka 并标记 published。
2. **设备真实下行**：device-svc 引入 MQTT broker（EMQX/HiveMQ），落 ADR-0012。
3. **LL-HLS 切片器**：实现 fMP4 + part + preload hint；浏览器侧补 hls.js 切换路径。
4. **WS 压测 + 多实例 hub**：把 Hub 拆分为 Redis Pub/Sub 协作，跑 5w 并发 baseline。
5. **管理员 UI**：admin-svc 加最小 React 页（安全参数、房间开关、设备开关）。
6. **KMS 接入**：把 JWT secret / device cert 改走 KMS；同时 user-svc/room-svc
   切 RS256。
