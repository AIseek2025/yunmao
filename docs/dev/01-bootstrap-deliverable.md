# 01-bootstrap 交付报告

> 本文记录第一轮"底座搭建"完成时的状态。后续若有大变更，请新增 02、03… 报告，不要覆盖。

## 1. 仓库新结构（树状摘要）

```
yunmao/
├── Makefile                            # fmt/lint/test/dev-up/poc-feed/bench-ws/gen-proto
├── README.md                           # 工程上手与端到端 PoC 步骤
├── docs/
│   ├── earlyproductplanning/           # 历史，仅参考
│   ├── finalproductplanning/           # 规划权威 00-11
│   └── dev/
│       ├── 00-bootstrap-notes.md
│       ├── 01-bootstrap-deliverable.md (本文)
│       └── adr/0001-0008-*.md
├── proto/
│   ├── README.md
│   ├── cloudevents/
│   │   ├── envelope.schema.json
│   │   ├── feed_command_requested.schema.json
│   │   └── feed_command_acked.schema.json
│   ├── errors/codes.yaml
│   ├── feeding/v1/feeding.proto
│   ├── device/v1/device.proto
│   ├── user/v1/user.proto
│   ├── room/v1/room.proto
│   └── gateway/signaling.md
├── rust/                               # Cargo workspace（resolver=2）
│   ├── Cargo.toml
│   ├── rustfmt.toml
│   ├── clippy.toml
│   └── crates/
│       ├── yunmao-common/              # ID/CloudEvents/error/telemetry
│       ├── yunmao-protocol/            # 信令 + MQTT + Kafka 事件 data 模型
│       ├── yunmao-ingest/              # RTMP 接入（rml_rtmp）+ FLV tag + 房间 pipe
│       ├── yunmao-media-edge/          # 房间扇出 + HTTP-FLV + QoE + Prometheus + ABR 占位
│       ├── yunmao-gateway/             # WebSocket Hub + 房间订阅 + 事件扇出
│       └── yunmao-device-edge/         # 设备模拟 + 按 device_id 串行调度 + idempotent
├── go/
│   ├── go.work
│   ├── sqlc.yaml                       # 模板，待启用
│   ├── migrations/0001_init.sql        # users/cats/rooms/devices/streams/feed_requests/...
│   ├── pkg/yunmao/                     # 公共库（独立 module）
│   │   ├── ids/ errors/ cloudevents/ httpx/ observability/
│   │   ├── feedstate/ idempotent/ cooldown/
│   │   └── go.mod
│   └── services/                       # 各服务独立 module，replace 指向 pkg/yunmao
│       ├── user-svc/
│       ├── room-svc/
│       ├── feeding-svc/                # 投喂状态机 + idempotent + cooldown + HTTP publisher
│       ├── device-svc/
│       ├── billing-svc/
│       └── admin-svc/
├── deploy/
│   ├── README.md
│   ├── docker-compose.dev.yml
│   └── observability/
│       ├── prometheus.yml
│       └── grafana/{provisioning,dashboards}/
├── scripts/
│   ├── gen-proto.sh                    # JSON/YAML schema 校验 + .proto 列表（buf 占位）
│   ├── bench-ws.sh                     # WebSocket 压测（websocat）
│   └── poc-feed.sh                     # 一次性触发投喂事件链路
└── .github/workflows/
    ├── rust.yml
    ├── go.yml
    └── proto.yml
```

## 2. 已实现模块清单

### Rust 数据面（`rust/crates/`）

| crate | 路径 | 职责 | 关键依赖 | 当前能力 | 测试 |
| --- | --- | --- | --- | --- | --- |
| yunmao-common | yunmao-common/ | DomainId(ULID + prefix) / ErrorCode / ErrorEnvelope / CloudEvent / telemetry | ulid, serde, thiserror, tracing-subscriber | ID 与错误码可序列化、JSON 日志 init | unit ok |
| yunmao-protocol | yunmao-protocol/ | WS 信令 / MQTT 设备协议 / Kafka 事件 data 模型 | serde, time | `ClientFrame`/`ServerFrame`/`DeviceCommand`/`FeedCommand*` 全部可序列化 | unit ok |
| yunmao-ingest | yunmao-ingest/ | RTMP 接入握手、AVC sequence header 解析、按 streamKey 路由 | rml_rtmp, tokio, bytes | RTMP publisher 可注册到 PublishRouter，FLV tag fan-out | unit ok |
| yunmao-media-edge | yunmao-media-edge/ | 房间扇出 / HTTP-FLV 出口 / QoE / Prometheus | axum, futures, metrics-exporter-prometheus | `GET /live/{room}.flv` 流式输出 + `POST /qoe` + `/metrics` | unit ok |
| yunmao-gateway | yunmao-gateway/ | WebSocket Hub、房间订阅、心跳、事件扇出、HTTP publish 入口 | axum, tokio, dashmap | `GET /ws` 完整握手、`POST /publish` 用于 feeding-svc 触发 | unit ok |
| yunmao-device-edge | yunmao-device-edge/ | 模拟设备：按 device_id FIFO 执行、随机延迟与失败、HTTP ack 回调 | tokio, rand | `POST /commands` 处理 FeedCommandRequested 并回调 ack URL | unit ok（含调度顺序断言） |

### Go 控制面（`go/`）

| 模块 | 路径 | 职责 | 关键依赖 | 当前能力 | 测试 |
| --- | --- | --- | --- | --- | --- |
| pkg/yunmao | go/pkg/yunmao | 共享 IDs / errors / CloudEvents / httpx / observability / feedstate / idempotent / cooldown | chi, prometheus client, ulid | TraceID 中间件、`/healthz`/`/readyz`/`/metrics`、状态机、幂等、冷却 | 7 个 sub-package unit ok |
| feeding-svc | go/services/feeding-svc | 投喂请求接收、状态机推进、幂等、冷却、HTTP 发布到 device-edge / gateway | pkg/yunmao | `POST /api/v1/feed-requests`、`POST /internal/feed-acks`、`GET /api/v1/feed-requests/{id}` | unit ok |
| user-svc | go/services/user-svc | SMS 登录 PoC、用户元数据 | pkg/yunmao | `POST /v1/auth/sms`、`GET /v1/users/{id}` | unit ok |
| room-svc | go/services/room-svc | 房间元数据 CRUD（内存）、`GET /v1/rooms` 列表 | pkg/yunmao | `GET/POST /v1/rooms`、`GET /v1/rooms/{id}` | unit ok |
| device-svc | go/services/device-svc | 设备状态查询 | pkg/yunmao | `GET /v1/devices`、`GET /v1/devices/{id}` | unit ok |
| billing-svc | go/services/billing-svc | 订单管理 PoC | pkg/yunmao | `POST /v1/orders`、`POST /v1/orders/{id}/pay` | unit ok |
| admin-svc | go/services/admin-svc | 投喂策略管理 | pkg/yunmao | `GET/PUT /v1/policies/feeding/{room_id}` | unit ok |

### 协议契约（`proto/`）

- gRPC 占位：`feeding/v1/`、`device/v1/`、`user/v1/`、`room/v1/`，未启用 codegen，待引入 buf。
- CloudEvents JSON Schema：`envelope.schema.json` + `feed_command_requested.schema.json` + `feed_command_acked.schema.json`。
- 错误码：`errors/codes.yaml`（与 `pkg/yunmao/errors` 和 `yunmao-common::error` 一致）。
- 信令：`gateway/signaling.md` 描述 WS JSON 协议（subscribe/unsubscribe/heartbeat/event）。

### 部署与可观测性（`deploy/`）

- `docker-compose.dev.yml`：Postgres / Redis / Kafka(KRaft) / MinIO / ClickHouse / Jaeger / Prometheus / Grafana。
- Prometheus 抓取 9 个 yunmao 服务端口。
- Grafana 自动加载 `Yunmao Overview` dashboard（活跃房间、网关连接、投喂请求率、状态迁移率）。

### CI（`.github/workflows/`）

- `rust.yml`：cargo fmt/check + clippy `-D warnings` + workspace test。
- `go.yml`：matrix 跑 7 个 module 的 vet+test，外加 `golangci-lint` 在 `go/` 上扫一次。
- `proto.yml`：`scripts/gen-proto.sh` 校验 JSON/YAML 与 `.proto` 文件存在。

## 3. 未完成 / TODO

| 项 | 原因 / 计划 |
| --- | --- |
| 真实 PG / Redis / Kafka 集成（Go 侧 pgx + sarama/segmentio kafka-go） | 控制面首版用内存 + HTTP 模拟事件总线，目的是先打通契约和状态机。第二阶段切换。 |
| sqlc 代码生成 | `go/sqlc.yaml` + `go/migrations/0001_init.sql` 已就位，待 PG 接入后跑 `sqlc generate`。 |
| LL-HLS 切片器 / WebRTC SFU | MVP 仅 RTMP→HTTP-FLV 兜底，符合 02 章首期路线。 |
| 真正的 ABR 转码 worker | `yunmao-media-edge::abr` 是 single-profile pass-through，预留 hook，需要 ffmpeg/ffmpeg-next 集成。 |
| 多设备真实 MQTT 接入 | `yunmao-device-edge` 当前是模拟器；MQTT broker / X.509 证书 / 凭证下发需补。 |
| Kafka producer/consumer 封装 | `pkg/yunmao` 与 `yunmao-common` 都没建 eventbus 模块，需 `rdkafka` / `franz-go` 选型。 |
| 完整 gRPC stub 生成 + grpc-gateway | proto 已就位但未生成 Rust/Go 代码，等 buf 引入。 |
| 真正 5 万连接级 ws-bench | 当前 `scripts/bench-ws.sh` 仅 PoC，正式压测建议 k6/Goose。 |
| KMS / Vault / 凭证管理 | 全部 dev 用户名密码，未上 Vault。 |
| 端到端 docker-compose 启动 yunmao 二进制 | 现在 docker-compose 只起基础设施，业务进程 host 直跑，后续可加 Dockerfile + compose service。 |

## 4. 关键技术决策（docs 没说但本次做了的）

1. **MVP 用内存 + HTTP 串联事件链路而非直接 Kafka**：feeding-svc 通过 HTTP POST 到 device-edge `/commands`、device-edge 回调 feeding-svc `/internal/feed-acks`、feeding-svc 推送 gateway `/publish`。这样不依赖 Kafka 即可演示状态机，CloudEvents 信封原样使用。**替代**：直接接 Kafka（更接近规划但启动门槛高）。**计划**：第二阶段加 `pkg/yunmao/eventbus` + `yunmao-common::eventbus`，把 HTTP publisher 替换为 Kafka producer。
2. **Go 各 service 用独立 module + replace 指向 `pkg/yunmao`**：
   - 优点：每个 service 可独立 `go mod tidy`，未来拆仓零成本；`go.work` 把所有 module 串起来本地开发依然顺滑。
   - 替代：单 module 多 service。但单 module 后续上 K8s 镜像分割、依赖隔离都要折腾。
3. **Rust workspace `resolver = "2"` + 共享 `[workspace.dependencies]`**：所有 crate 用 `something.workspace = true` 引用，避免版本漂移。
4. **`yunmao-media-edge` 用 `futures::stream::unfold` 实现 FLV 流**：替代手写 `Stream` 实现，避免 `Pin<&mut impl Future>::poll` 的 lifetime 噩梦。
5. **`yunmao-device-edge` 串行调度 per device**：用 `tokio::sync::mpsc::UnboundedSender<Command>` + 每 device 一个 worker task，配合 idempotent set 完成 04 章 4.4 的 FIFO 与单粮约束。
6. **`pkg/yunmao/feedstate` 把 04 章 4.4 状态机抽成纯函数 + 表驱动**：feeding-svc 内存 store 直接复用，未来切 PG 实现也复用同一张转移表。
7. **Go 版本回退到 1.23**：第一次装的 `go1.26.3` 安装包文件出现混合版本（部分文件标准库声明被覆盖，标准库无法 vet 通过）。回退到稳定的 1.23.4。所有 module 与 `go.work` 对齐到 `go 1.23`。

## 5. 本地启动指南

```bash
# 0. 工具链
export PATH="$HOME/.local/go/bin:$HOME/.cargo/bin:$PATH"
go version    # go1.23.4 darwin/arm64
cargo --version

# 1. 起基础设施
make dev-up

# 2. 编译并跑 Rust 边缘 + Go 控制面（建议 6 个终端 / tmux）
cd rust && cargo run --release --bin yunmao-media-edge
cd rust && cargo run --release --bin yunmao-gateway
cd rust && YUNMAO_FEED_ACK_URL=http://127.0.0.1:8201/internal/feed-acks cargo run --release --bin yunmao-device-edge
cd go/services/feeding-svc && go run ./cmd/feeding-svc
cd go/services/room-svc    && go run ./cmd/room-svc
cd go/services/user-svc    && go run ./cmd/user-svc

# 3. 推 RTMP 流到 ingest
ffmpeg -re -f lavfi -i testsrc=size=1280x720:rate=30 \
       -f lavfi -i sine=frequency=1000 \
       -c:v libx264 -tune zerolatency -preset veryfast -b:v 2000k \
       -c:a aac -b:a 96k -f flv rtmp://127.0.0.1:1935/live/room_demo

# 4. 浏览器拉流（mpegts.js / flv.js）：http://127.0.0.1:8080/live/room_demo.flv

# 5. 触发投喂端到端
ROOM_ID=room_demo bash scripts/poc-feed.sh

# 6. 用 websocat 订阅事件
websocat ws://127.0.0.1:8090/ws
> {"op":"subscribe","rooms":["room_demo"]}
# 等待 feed.command.requested → feed.command.acked 事件流过
```

## 6. 验证命令与结果摘要

2026-05-24 复检结论：**部分通过，偏通过**。代码可编译、核心单测通过，Rust/Go/proto 验证命令在补齐本机 PATH 后全部通过；但仍有若干 PoC 可执行性和生产化风险，需要在第二阶段处理。

| 命令 | 工作目录 | 结果 |
| --- | --- | --- |
| `cargo fmt --all -- --check` | `rust/` | OK。首次 shell 环境缺少 `~/.cargo/bin` 导致 `cargo` 不可见，按 Makefile PATH 补齐后通过。 |
| `cargo clippy --workspace --all-targets -- -D warnings` | `rust/` | OK |
| `cargo test --workspace` | `rust/` | OK（yunmao-common 8、yunmao-protocol 5、yunmao-ingest 9、yunmao-media-edge 5、yunmao-gateway 4、yunmao-device-edge 5；doc-test 全部通过） |
| `make go-vet` | 仓库根 | OK（7 module） |
| `make go-test` | 仓库根 | OK（pkg/yunmao 7 子包 + 6 service `internal/service` 全部 ok） |
| `bash scripts/gen-proto.sh` | 仓库根 | OK（3 JSON schema + 1 YAML + 4 .proto 文件枚举） |

## 7. 复检发现的问题 / 风险

| 严重程度 | 路径 | 结论与建议 |
| --- | --- | --- |
| 高 | `scripts/poc-feed.sh`、`README.md` | 已修正。脚本原先调用不存在的 `POST /v1/feeds`、默认端口 `8101`，且没有把 `idempotency_key` 放进 JSON body，按文档执行会失败；现已对齐 `POST /api/v1/feed-requests`、端口 `8201`。 |
| 高 | `scripts/bench-ws.sh`、`docs/dev/01-bootstrap-deliverable.md`、`README.md` | 已修正。WebSocket 实现是 `ws://127.0.0.1:8090/ws`，订阅帧是 `{"op":"subscribe","rooms":[...]}`；旧文档和脚本写成 `8081/v1/ws` 与 `room_id` 单字段。 |
| 中 | `go/services/feeding-svc/internal/service/service.go` | 当前默认 `cooldown.New(15s, 60s, 100g)` 与交付报告中 `15s/30s/20` 不一致，也不够保守。见 ADR-0006，建议第二阶段改为配置化并采用 `30s/60s/12g` 作为 MVP 默认。 |
| 中 | `go/services/feeding-svc/internal/service/service.go`、`rust/crates/yunmao-device-edge/src/server.rs` | 投喂链路形成 PoC，但仍是内存状态 + HTTP 模拟事件总线；进程重启丢状态，ack 失败不重试。第二阶段必须接 PG/Redis/Kafka，并保留设备指令幂等。 |
| 中 | `proto/`、`scripts/gen-proto.sh` | proto 目前只做语法存在性和 JSON/YAML 解析，不做 `buf lint`、breaking check 或代码生成。见 ADR-0005，建议先引入 Buf CLI，本地和 CI 做 lint/breaking，BSR 暂缓。 |
| 中 | `rust/crates/yunmao-gateway/src/server.rs` | WS 网关目前无鉴权，游客可订阅、可发聊天 PoC 帧；上线前必须接 user-svc JWT 与 room-svc 短期订阅 token。见 ADR-0007。 |
| 低 | `.github/workflows/go.yml` | CI 与目录一致；但 `golangci-lint` 依赖 `go/.golangci.yml`，若后续没有配置文件会失败。当前本地复检未运行 GitHub Actions。 |

## 8. 架构负责人建议

以下 7 项从“待确认”调整为架构建议 / 临时决策，业务最终值仍需在 Phase 0/Phase 1 评审确认。对应 ADR 已落到 `docs/dev/adr/0002-0008-*.md`。

| 问题 | 推荐值 | 理由 | 替代方案 | 重新评估条件 |
| --- | --- | --- | --- | --- |
| 直播协议 | MVP 仅做 RTMP 输入 + HTTP-FLV 输出；预留 LL-HLS 切片器接口，WebRTC 暂缓 | 当前 Rust PoC 已形成 RTMP→HTTP-FLV 链路，最小闭环清晰；LL-HLS/WebRTC 会显著增加播放器、CDN、SFU 和移动端验证成本 | 首期直接云直播 HLS/LL-HLS，或直接 WebRTC | iOS/H5 兼容性不可接受、投喂强反馈必须 <1s、云厂商低延迟 SDK 已确定 |
| 事件总线 | Kafka | CloudEvents、Schema Registry、ClickHouse/Flink、招聘与运维生态更稳；deploy 已有 Kafka | RocketMQ 作为中国云厂商托管或团队强经验备选 | 团队已有 RocketMQ SRE 能力、云厂商托管成本/稳定性明显更优 |
| 主数据库 | PostgreSQL 起步，不直接上 CockroachDB | MVP 单 region/多 AZ 足够；CRDB 会提前引入事务、延迟和运维复杂度 | CockroachDB、TiDB、MySQL | Phase 2 出现明确多 region 强一致写入，且团队具备分布式 SQL 运维能力 |
| Proto 工具链 | 引入 Buf CLI；BSR 暂缓 | 本地 lint/breaking 先解决漂移问题，不把早期协作绑定到远端注册表 | 继续手写、直接上 BSR | 多仓并行、外部 SDK 发布、跨团队依赖增加 |
| 投喂安全默认值 | MVP 默认更保守：room 30s、user-room 60s、cat daily 12g，并后台可调 | 动物福利和舆论风险优先，默认值应保守；当前实现与文档不一致 | 维持 15s/60s/100g 或按运营活动放宽 | 兽医/运营给出猫只画像、设备出粮精度和房间差异化策略 |
| WS 鉴权 | user-svc 签登录 JWT；room-svc 签短期 room subscription token；游客只读公开房间和有限事件 | 登录身份与房间权限分离，便于 App/Web 共用；游客可降低首访门槛但不允许写操作 | 网关直接验长 JWT；完全禁止游客 | 需要低成本匿名互动、或上 CDN/边缘 token 统一方案 |
| clients demo | 立即添加 `clients/web-demo` 最小演示 | 作为直播/WS/投喂链路回归工具，不作为正式前端 | 暂不做客户端，继续 curl/websocat | Rust/Go 链路进入联调、需要 QA/产品自测、要修复播放/WS 端到端回归 |
