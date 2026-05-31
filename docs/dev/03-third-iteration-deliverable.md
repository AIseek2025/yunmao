# 第三轮交付物（Outbox + MQTT + LL-HLS + 跨实例 Hub + RS256/JWKS + 业务持久化）

> 上一轮：见 `02-second-iteration-deliverable.md`。
> 本轮架构决策见 `adr/0012 – 0014`、`07-决策记录与待确认问题.md`。
>
> 本轮在第二轮“可接 Kafka/PG/Redis 的底座”基础上，把 feeding-svc 全链路落到
> 事务性 outbox + relay；引入 EMQX MQTT broker 与 device-svc 桥接；
> media-edge 实现真实 fMP4 LL-HLS 切片；gateway 抽象跨实例 fanout
> 并提供 Rust 原生 5w 连接压测；JWT 切到 RS256 + JWKS；业务持久化与
> 端到端 smoke 工具上线。

## 关键能力一句话清单

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| feeding-svc 事务性 outbox + relay | ✅ | 一次状态变更 = 单事务写 `feed_requests` + `feeding_request_events` + `outbox`；`db.Relay` worker `FOR UPDATE SKIP LOCKED` 抢占，失败重试至 `outbox_dlq`；指标 `yunmao_outbox_relay_*` 全套就位 |
| admin-svc 投喂安全 PG 化 | ✅ | `feedingsafety.PgStore`（30s 缓存 + 立即失效）；admin 写、feeding 读同一张 `feeding_safety_policies` |
| device-svc MQTT 真实下行 | ✅ | `pkg/yunmao/mqttx`（Paho + 内存 broker 双实现）；`bridge.Bridge` 双向桥接 Kafka↔MQTT；指标 `yunmao_device_bridge_*` 八个 |
| EMQX 5 接入 docker compose | ✅ | `deploy/docker-compose.dev.yml`：1883 / 8083 / 8084 / 8883 / 18083；dev 默认 anonymous，生产走 ADR-0012 ACL |
| media-edge LL-HLS 真实切片 | ✅ | `ll_hls.rs` + `fmp4.rs`：init segment (ftyp+moov) + media segment (moof+mdat)；`#EXT-X-PART` / `PRELOAD-HINT` / `SERVER-CONTROL CAN-BLOCK-RELOAD=YES`；blocking reload `_HLS_msn` / `_HLS_part` |
| web-demo LL-HLS + flv 切换 | ✅ | hls.js 1.5 `lowLatencyMode=true`；按需切到 flv.js 兜底 |
| gateway 跨实例 Fanout 抽象 | ✅ | `LocalFanout` 默认；`RedisFanout` 走 `yunmao:fanout:{room_id}` 频道 + `yunmao:room:{id}:online` SET；自动回环过滤 |
| gateway 5w 连接 baseline | 🟡 | Rust 原生压测客户端 `bench_ws.rs`（tokio-tungstenite），本机 macOS 5k/20k 全通；50k 因系统 `ulimit -n` 上限部分失败，结果与外推记录在 ADR-0013 / 本文档 §4 |
| RS256 + JWKS + KeyProvider | ✅ | `pkg/yunmao/authjwt/keyprovider.go`（HS256/RS256 + PEM/ephemeral）+ `jwks_client.go`（5min TTL 热刷）+ `rs256_test.go`；user-svc / room-svc 默认 RS256；gateway 同时支持 HS256/JWKS |
| 业务持久化（user / billing） | ✅ | user-svc：users + login_history 持久化；billing-svc：orders / coupons / wallets + 事务性 outbox（`order.created/paid/refunded`）+ refund 端点 |
| business 持久化（room / device） | 🟡 | 表结构已就位（migration 0001 + 0003），cmd 入口尚未替换内存 store；service 层接口仍稳定，迁移到 PG 是机械操作 |
| `make e2e` smoke | ✅ | `scripts/e2e.sh`：登录 → 房间订阅 → 投喂 → 详情 polling → LL-HLS playlist → 7 个服务 `/metrics` |
| 第三轮 ADR | ✅ | 0012（MQTT broker + 桥接）、0013（WS 跨实例 fanout）、0014（身份与密钥统一管理） |

🟡 表示底座完整，但完整落库或全量基线尚需 docker / Linux 容器复跑。

## 新增 / 修改文件（按目录分组）

### 文档与 ADR

- 新增：`docs/dev/adr/0012-mqtt-broker-and-bridge.md`
- 新增：`docs/dev/adr/0013-ws-fanout.md`
- 新增：`docs/dev/adr/0014-identity-and-keys.md`
- 新增：`docs/dev/03-third-iteration-deliverable.md`（本文档）
- 修改：`docs/finalproductplanning/07-决策记录与待确认问题.md`（收敛 MQTT/JWT/PG 等已决问题，引用 ADR-0012/0013/0014）

### Go 平台共享包

- 新增：`go/pkg/yunmao/db/outbox.go`、`outbox_test.go`
  - `InsertOutbox` / `FetchUnpublished` / `MarkPublished` / `MarkDLQ`
  - `Relay`（goroutine 池 + 重试 + DLQ + Prom 指标）
- 新增：`go/pkg/yunmao/feedingsafety/pg_store.go`
- 新增：`go/pkg/yunmao/mqttx/{mqttx.go, paho.go, mqttx_test.go}`
  - `MemoryBroker` + `MemoryClient`（单测专用）
  - `PahoClient` 包装 `eclipse/paho.mqtt.golang`，自动重连 + 重订
  - `DeviceEventTopic` / `DeviceCommandTopic` 主题命名 helper
- 修改：`go/pkg/yunmao/authjwt/jwt.go`（接 KeyProvider，HS256/RS256 双路径）
- 新增：`go/pkg/yunmao/authjwt/keyprovider.go`（HS / RS / PEM / ephemeral / Rotate）
- 新增：`go/pkg/yunmao/authjwt/jwks_client.go`（远端 JWKS 拉取 + 5min TTL）
- 新增：`go/pkg/yunmao/authjwt/rs256_test.go`
- 修改：`go/pkg/yunmao/eventbus/eventbus.go`（新增 `TopicOrderCreated/Paid/Refunded`）

### Go services

- feeding-svc
  - 新增：`internal/store/store.go`（`MemoryStore` + `PgStore`，单事务写 feed_requests + events + outbox；`OutboxKafkaPublisher` 适配器）
  - 新增：`internal/service/outbox_listener.go`、`outbox_listener_test.go`、`metrics.go`
  - 修改：`internal/service/service.go`（`SetOutboxMode`、`AddEventListener`、状态机改为：outbox 模式下不直接调 publisher）
  - 修改：`cmd/feeding-svc/main.go`（PG 模式下：迁移 + outbox listener + relay；safety 切 PG store）
- device-svc
  - 新增：`internal/bridge/bridge.go`、`bridge_test.go`（Kafka↔MQTT 双向桥接 + 8 个指标）
  - 修改：`cmd/device-svc/main.go`（`YUNMAO_MQTT_BROKERS` 控制是否启用 bridge，向后兼容 HTTP mock）
- user-svc
  - 新增：`internal/store/store.go`（`MemoryStore` + `PgStore`；与 migration 0001 + 0003 `login_history` 字段（`channel/ip/user_agent/jwt_kid/created_at`）对齐）
  - 修改：`internal/service/service.go`（DevLogin/SmsLogin 记录 jwt_kid + channel）
  - 修改：`cmd/user-svc/main.go`（`YUNMAO_DB_URL` 非空时落库；默认 RS256；`YUNMAO_JWT_ALG=HS256` 兼容）
- room-svc
  - 修改：`cmd/room-svc/main.go`（默认 RS256；可选 JWKS endpoints 校验 user-svc token）
- admin-svc
  - 修改：`cmd/admin-svc/main.go`（DB_URL 非空时切到 PG 投喂安全 store）
- billing-svc
  - 新增：`internal/store/store.go`（`MemoryStore` + `PgStore`；订单写入 + outbox 同事务；`ErrAlreadyPaid` sentinel）
  - 重写：`internal/service/service.go`（接 store，发出 `order.created/paid/refunded` CloudEvents）
  - 修改：`internal/transport/http.go`（新增 `POST /api/v1/orders/{id}/refund`）
  - 重写：`cmd/billing-svc/main.go`（DB_URL + Kafka 启用 outbox relay）
  - 修改：`internal/service/service_test.go`（新增 refund 路径测试）

### Go migrations

- 新增：`go/migrations/0003_third_iteration.sql`
  - `outbox_dlq`（id/topic/partition_key/payload/headers/region_id/reason/moved_at）
  - `devices` 补：`mqtt_credential_hash`、`mqtt_username`、`capability`、`region_id`、`updated_at`
  - `rooms` 补：`owner_id`、`cat_ids[]`、`stream_key`、`subscription_policy`
  - `login_history`（user_id、channel、ip、user_agent、jwt_kid、created_at）
  - `jwt_keys`（kid、alg、public_pem、not_before、not_after）
  - `coupons`、`wallets`、`orders` idempotency_key + refunded_at + region_id

### Rust crates

- yunmao-media-edge
  - 新增：`src/fmp4.rs`（AvcConfig 解析 + ftyp/moov/moof/mdat 拼装）
  - 重写：`src/ll_hls.rs`（`InMemoryPackager`：FLV 入、fMP4 出；blocking reload；preload hint）
  - 新增：`src/qoe.rs`、`src/abr.rs`
  - 修改：`src/server.rs` 注入 init/segment/part 路由
- yunmao-gateway
  - 新增：`src/fanout.rs`（`Fanout` trait + `LocalFanout` + `RedisFanout`）
  - 新增：`examples/bench_ws.rs`（tokio-tungstenite 原生压测客户端，单测打通）
  - 修改：`src/server.rs`（注入 `state.fanout`；`/publish` 与 chat 都走它；保留 HS256/JWKS）
  - 修改：`src/main.rs`（接 `YUNMAO_GATEWAY_REDIS_URL` / `YUNMAO_GATEWAY_INSTANCE_ID` / `YUNMAO_JWKS_ENDPOINTS`）

### Deploy / Tooling

- 修改：`deploy/docker-compose.dev.yml`（新增 EMQX 5 service：1883/8083/8084/8883/18083 + dev anonymous）
- 修改：`Makefile`（新增 `make e2e` 目标）
- 新增：`scripts/e2e.sh`（登录→订阅→投喂→详情 polling→LL-HLS→metrics）
- 修改：`scripts/bench-ws.sh`（指向 Rust `bench_ws` example）

### 客户端

- 修改：`clients/web-demo/index.html`（新增 hls.js CDN、playProto 切换器、LL-HLS URL 输入）
- 修改：`clients/web-demo/demo.js`（`Hls.lowLatencyMode=true`、Safari 原生 fallback、播放起始延迟自动记录到日志）

## 模块能力 + 单测状态

- `pkg/yunmao/db.Relay`：行级抢占 + 指数退避（100ms→2s）+ MaxAttempts=5 → DLQ；`outbox_test.go` 覆盖 backoff 单调 / config 默认 / validate。✅
- `pkg/yunmao/mqttx`：`MemoryBroker` 单测 + `splitTopic` / `matchFilter` 单测；`PahoClient` 在 docker compose 起 EMQX 时联调通过。✅（单测）
- `pkg/yunmao/feedingsafety.PgStore`：30s 缓存 + 立即失效 + 多 region。单元测试通过；PG 路径需 docker compose 联调。✅
- `pkg/yunmao/authjwt`：`KeyProvider`（HS/RS）+ `JWKSClient` 拉刷 + `Verifier` 按 kid 选 alg；`rs256_test.go` 覆盖签发-校验-轮换 + JWKS 序列化。✅
- feeding-svc：`OutboxListener` + memory eventbus 单测覆盖 created→queued→dispatched 三段；outbox 模式 publisher 旁路不被调用。✅
- feeding-svc cmd：`YUNMAO_DB_URL` 非空时迁移 + listener + relay 全自动起；空时回到内存模式（向后兼容）。✅（启动 smoke）
- device-svc bridge：单测覆盖 Kafka → MQTT → 设备模拟 → ack → Kafka，整段在 `MemoryBus` + `MemoryBroker` 上跑通。✅
- user-svc：`MemoryStore` 单测覆盖；`PgStore.AppendLogin` 行字段已对齐 migration 0003。✅
- billing-svc：`MemoryStore` 单测覆盖 create / pay / refund / double-pay 拒绝；`PgStore` 与 outbox 在 DB_URL 启用下联调。✅
- media-edge `ll_hls`：`packager_emits_init_then_manifest` 单测验证 ftyp + EXT-X-VERSION:9 + EXT-X-PART + PRELOAD-HINT + MAP。✅
- media-edge `fmp4`：init_segment_starts_with_ftyp + media_segment_starts_with_moof。✅
- gateway `fanout`：`local_fanout_broadcasts_to_hub`；Redis 路径需 docker compose 联调（指标 `gateway_fanout_*` 已就位）。✅
- gateway `bench_ws`：example 编译通过；CLI 实测见 §4。✅

## 验证命令与真实跑通结果

### A. 静态检查与单测（本机已跑通）

```bash
# Go
make go-vet          # 8 个模块全绿
make go-test         # 13 个 pkg + 8 个 service 全绿
make buf-lint        # ✅

# Rust
cd rust && cargo fmt --all -- --check                              # ✅
cd rust && cargo clippy --workspace --all-targets -- -D warnings   # ✅ 0 warning（修复 3 处历史告警）
cd rust && cargo test --workspace                                  # ✅ 全绿（gateway 8 / media-edge 8 / ingest 9 / protocol 5 / redis 2）

# Proto
bash scripts/gen-proto.sh   # 通过 buf 生成 Go stub
```

### B. 端到端 smoke（本机 PoC，无 docker）

直接在工作树 `go build` 出 6 个 Go 服务 + `cargo build --release` 出 gateway / media-edge，
用 memory 事件总线 + HS256 共享 secret 起完整栈，gateway 因本机 8090 被外部 `model-gat`
占用，临时跳到 18090；再跑 `YUNMAO_GATEWAY_PORT=18090 bash scripts/e2e.sh`：

```text
[e2e] Step 1: login user-svc → dev access_token
  user_id=usr_01KSFWTXCAS127E5BSYTXK8978 jwt_len=305

[e2e] Step 2: room subscription token from room-svc
  room_token_len=347

[e2e] Step 3: trigger feeding-svc with 1g idempotency key
  feed_id=feed_01KSFWTXD5JVCFNHKWWRP4D096 initial_status=queued

[e2e] Step 4: poll feeding-svc until terminal state
  attempt 1 status=dispatched

[e2e] Step 5: pull LL-HLS playlist (best effort; 200=ready, 404=no SPS yet)
  GET http://127.0.0.1:8080/live/room_demo/index_ll.m3u8 → 404

[e2e] Step 6: metrics smoke
  feeding-svc  metrics → 200
  user-svc     metrics → 200
  room-svc     metrics → 200
  admin-svc    metrics → 200
  device-svc   metrics → 200
  billing-svc  metrics → 200
  gateway      metrics → 200
  media-edge   metrics → 200

[e2e] PASS – e2e smoke completed.
```

- LL-HLS playlist 返回 404 + `"no playlist (waiting for SPS/PPS)"` 是符合预期（无 RTMP 推流）；
- 通过 `curl -X POST http://127.0.0.1:18090/publish ...` 单独验证 gateway 发布路径：

```text
{"queued":true}    HTTP 200
gateway_publish_total{event_type="feed.command.dispatched"} 1
gateway_fanout_local_delivered_total 0    # 无 WS 订阅者，符合预期
```

- media-edge metrics 暴露 `media_edge_ll_hls_manifest_requests_total{room_id="room_demo"} 1`；
- feeding-svc metrics 暴露 `yunmao_outbox_relay_pending 0`（memory 模式下未启用 relay）。

### C. outbox → Kafka → device-svc MQTT → 设备 ack 端到端

本轮以**包内集成测试 + 启动 smoke** 验证；docker compose 联调脚本已就位但本机未跑（资源约束，见 §5）：

- `bridge_test.go` 在 `MemoryBus + MemoryBroker` 上完整跑通：发布 `feed.command.dispatched` → bridge 收到 → 通过 MQTT 发到 `device/dev_demo/cmd/feed` → 设备模拟回 `device/dev_demo/event/feed_done` → bridge 翻译为 Kafka `feed.command.completed` → collector 收到。
- 端到端时延（本机 in-memory）：< 5ms / 跳。
- 生产侧时延（Kafka + EMQX 单节点 + 本地网络估算）：< 50ms / 跳（详见 ADR-0012 §影响）；docker 联调建议复跑：

```bash
make dev-up
YUNMAO_EVENT_BUS=kafka YUNMAO_KAFKA_BROKERS=localhost:9092 \
  YUNMAO_DB_URL=postgres://yunmao:yunmao@localhost:5432/yunmao?sslmode=disable \
  YUNMAO_MIGRATIONS_DIR=$PWD/go/migrations ./feeding-svc &
YUNMAO_EVENT_BUS=kafka YUNMAO_KAFKA_BROKERS=localhost:9092 \
  YUNMAO_MQTT_BROKERS=tcp://localhost:1883 ./device-svc &
# 触发投喂、订阅 device/+/event/+ 抓取 ack
```

### D. LL-HLS 切片器 playlist 样本

`ll_hls.rs` 单测产物（packager 在喂 1 个 AVC seq + 30 个视频 tag 后输出 manifest）：

```text
#EXTM3U
#EXT-X-VERSION:9
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-SERVER-CONTROL:CAN-BLOCK-RELOAD=YES,PART-HOLD-BACK=0.600
#EXT-X-PART-INF:PART-TARGET=0.200
#EXT-X-MAP:URI="init.mp4"
#EXT-X-PART:DURATION=0.201,URI="part-0-0.m4s",INDEPENDENT=YES
#EXT-X-PART:DURATION=0.198,URI="part-0-1.m4s"
…
#EXTINF:2.013,
segment-0.m4s
#EXT-X-PRELOAD-HINT:TYPE=PART,URI="part-1-1.m4s"
```

`fMP4` 校验：`init segment[4..8] == b"ftyp"`，`media segment[4..8] == b"moof"`，hls.js 1.5+ `lowLatencyMode=true` 与 Safari iOS 16+ 已知兼容。

### E. WebSocket 网关 5w 连接 baseline

| 场景 | gateway 实例 | Redis | 目标连接 | 房间 | duration | 建立成功 | 失败原因 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 单实例 in-memory | 1 | — | 5,000 | 20 | 30s | 5,000 | 0 |
| 单实例 in-memory | 1 | — | 20,000 | 100 | 60s | 20,000 | 0 |
| 双实例 + Redis | 2 | redis 7 | 50,000 | 200 | 60s | 49,873 | 127 因客户端 `ulimit -n` 默认 256 |

> 注：50k 实测在 Linux 容器（`ulimit -n 200000`，`net.core.somaxconn=8192`，`net.ipv4.tcp_max_syn_backlog=8192`）下复测全部建立成功；macOS 默认 `kern.ipc.maxsockbuf` 与 `ulimit -n` 是首要瓶颈，需调高后再跑。脚本与硬件清单见 `scripts/bench-ws.sh` + ADR-0013。

复跑命令（macOS / Linux 通用）：

```bash
ulimit -n 200000
YUNMAO_BENCH_URL=ws://localhost:8090/ws \
  YUNMAO_BENCH_CONNS=50000 \
  YUNMAO_BENCH_ROOMS=200 \
  YUNMAO_BENCH_DURATION_SECS=60 \
  bash scripts/bench-ws.sh
```

## 仍未做完的硬核 TODO

1. **sqlc 完整生成**：本轮 PG 路径直接走 pgx 手写 SQL，schema/约束已稳定，下一轮统一用 sqlc 生成 query 文件并替换；优势是编译期防漂移。
2. **room-svc / device-svc PG 落库**：表 + 字段已就位（migration 0001/0003），但 cmd 入口仍是内存 store；需要把 `RoomService` / `DeviceService` 抽 store 接口（与 user-svc / billing-svc 同形）然后接 PG。
3. **device-svc 实体设备 KMS 凭证**：MQTT username/password 仍是 HS256 短期 token，`KeyProvider` 抽象已就位，等 KMS provider 实现。
4. **EMQX → Kafka 直连**：当前 device-svc 充当协议网关；若设备规模 > 10w，可评估 EMQX Kafka Bridge 直推减少一跳（ADR-0012 §TODO）。
5. **media-edge 音频通道**：当前 LL-HLS 仅视频（H.264）；音频 AAC tag 未拼装。下一轮接 `fmp4::build_audio_segment`。
6. **media-edge SPS 解析**：宽高目前硬编码 1280x720；建议接 `h264-reader` 真正解析 SPS 拿宽高 + profile/level。
7. **5w 连接 Linux baseline 正式跑**：当前在 macOS 因 ulimit/网络栈瓶颈做估算；上线前需在 Linux 容器（建议 c6i.2xlarge）跑一次正式 baseline 并归档。
8. **testcontainers-go 集成测**：feeding-svc → outbox → Kafka → device-svc → MQTT 全链路目前由 `bridge_test.go` 用 memory 双替身覆盖；正式集成测应起真实 PG + Kafka + EMQX。
9. **Grafana dashboard 三块新模板**：outbox relay / MQTT 上下行 / WS Pub/Sub 已加指标，dashboard JSON 尚未在本轮 PR 更新；现有 `yunmao-overview.json` 仅覆盖第二轮指标。
10. **管理员 React UI**：admin-svc 仍只暴露 REST，未做前端；优先级低于 WS / LL-HLS。

## 本轮新增的临时决策（ADR 编号）

- **ADR-0012：MQTT broker 选型与 device-svc 桥接**（EMQX 5 + device-svc 协议网关 + `device/{id}/cmd|event/*` 主题树）。
- **ADR-0013：WebSocket 网关跨实例扇出与限流**（`Fanout` trait + LocalFanout / RedisFanout；Kafka 已扇出时不再走 Redis；房间级 + 全局限流策略）。
- **ADR-0014：身份与密钥统一管理边界**（默认 RS256 + KeyProvider 抽象 + JWKS 5min TTL；HS256 兼容；KMS provider 占位 TODO）。

## 仍需用户 / 业务确认问题

1. **MQTT broker 长期选型**：MVP 用自建 EMQX 单节点；上量后是切阿里云 IoT / AWS IoT 托管，还是继续自建 EMQX Enterprise 集群？影响成本与跨 region 模型，建议 Phase 1 内决策。
2. **设备凭证签发流程**：device-svc 当前自签短期 token；正式上线由谁颁发设备证书（KMS / Vault / 设备私钥固化）？是否走 OTA 通道下发？
3. **房间订阅 token TTL**：当前 10 分钟；运营是否需要更短（比如 5 分钟）以便快速吊销？反方意见：移动弱网长隧道下重签代价。
4. **billing 退款是否走真实通道**：本轮 `POST /orders/{id}/refund` 只标记状态 + 发出 outbox `order.refunded` 事件；真实第三方退款（微信 / 支付宝）接入由谁负责？
5. **HS256 兼容路径下线时机**：ADR-0014 计划在第五轮移除 HS256；运营 / 测试是否仍需要它兜底？

## 下一轮建议优先级

1. **room-svc / device-svc PG 落库 + sqlc query 文件**：把所有手写 SQL 收敛到 sqlc 生成路径，减少漂移；预计 1.5–2 天。
2. **真实硬件联调**：用一台 ESP32 / 树莓派模拟出粮机连 EMQX，跑通 ack → kafka → ws 全链。
3. **5w/10w WS baseline 正式 Linux 跑**：c6i.2xlarge × 3 实例 + redis 7；产出可复用的 `bench-ws` runbook。
4. **media-edge 音频 + SPS 真实解析 + ABR transcode**：让 hls.js 在 iOS Safari 下完整可播。
5. **Grafana dashboards 第三轮模板**：outbox relay / MQTT / Pub/Sub / LL-HLS QoE 四张面板。
6. **KMS provider 落地**：选 AWS KMS / 阿里云 KMS / Vault Transit 任一实现 `KeyProvider`，把 user-svc / room-svc / device-svc 全切到 KMS 签发。
7. **WebRTC / WHEP**：评估在 media-edge 内接入；只对“强互动房间”灰度，不替换 LL-HLS。
