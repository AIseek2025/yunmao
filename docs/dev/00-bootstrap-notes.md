# 00-bootstrap 笔记

> 本文不属于 `docs/finalproductplanning/`，是底座搭建过程的开发日志，可随时迭代。

## 1. 现状速记（动手前）

- 仓库根目录原本只有 `docs/`：
  - `docs/finalproductplanning/`（00–11，规划权威，不可改）。
  - `docs/earlyproductplanning/`（历史背景，仅参考）。
- `cargo`/`rustc` 1.94.1 已就绪；`go` 未安装，第一次试装的 `go1.26.3 darwin/arm64` 包文件混乱（标准库出现 `digest`/`chunk` 重复定义错误），最终落到 `go1.23.4 darwin/arm64` 到 `~/.local/go`，使用前 `export PATH="$HOME/.local/go/bin:$PATH"`。所有 module/`go.work` 的 `go` directive 也对齐到 1.23。
- 本机已具备 `docker`、`docker-compose`、`ffmpeg`；缺 `protoc` / `buf`，因此 proto codegen 走 `prost` 内置编译，Go 端通过纯文本 schema 模拟（标准 protoc 未必每个开发机都有），生成脚本为可选项。
- 仓库根的 `CLAUDE.md` 在 `/Users/brando/Documents/trae_projects/CodeMaster/CLAUDE.md`，要求 `rust/` 工作区使用 `cargo fmt`、`cargo clippy --workspace --all-targets -- -D warnings`、`cargo test --workspace`，本仓库（`isolated_autoruns/yunmao`）按相同精神管理。

## 2. 落地的目录蓝图

按 `08-多端工程组织与协作.md` 第 2 节，但 MVP 阶段先采用“单一 monorepo + 子目录”形态，避免一开始就拆多仓：

```
yunmao/
├── docs/
│   ├── finalproductplanning/    # 不可改
│   ├── earlyproductplanning/    # 历史
│   └── dev/                     # 本目录，开发期日志/ADR
├── rust/                        # Cargo workspace（数据面、媒体边缘、网关）
│   ├── Cargo.toml
│   ├── rust-toolchain.toml
│   ├── rustfmt.toml
│   └── crates/
│       ├── yunmao-common/
│       ├── yunmao-protocol/
│       ├── yunmao-ingest/       # RTMP 接入
│       ├── yunmao-media-edge/   # 房间扇出 + HTTP-FLV 出口
│       ├── yunmao-gateway/      # WebSocket 网关
│       └── yunmao-device-edge/  # 设备边缘 / 投喂模拟
├── go/                          # Go workspace（控制面 / 业务）
│   ├── go.work
│   ├── pkg/yunmao/              # 共享 ID / 错误码 / CloudEvents / middleware
│   ├── services/
│   │   ├── user-svc/
│   │   ├── room-svc/
│   │   ├── feeding-svc/
│   │   ├── device-svc/
│   │   ├── billing-svc/
│   │   └── admin-svc/
│   └── migrations/              # SQL 迁移
├── proto/                       # 共享契约：CloudEvents schema + .proto
├── deploy/
│   └── docker-compose.dev.yml
├── scripts/
│   ├── gen-proto.sh
│   ├── bench-ws.sh
│   └── poc-feed.sh
├── .github/workflows/           # CI 草稿
├── Makefile                     # make fmt/lint/test/dev-up/dev-down/…
└── README.md
```

## 3. 边界与约定

- **Rust 数据面 vs Go 控制面**（按 01 第 3 节）：用户请求路径上没有 Rust；Rust 通过 Kafka/MQTT 与 Go 协作；Go 不直接处理媒体包。
- **事件总线契约**：本次 MVP 选 Kafka（兼容 RocketMQ 协议但开源工具链最友好）。所有 topic 统一 CloudEvents 1.0 信封，`type` 与 topic 一致；`subject` 携带业务主键。
- **ID 规范**：严格按 11 第 7 节，`{prefix}_{ulid}`。Rust 与 Go 共享生成函数。
- **错误码**：4 章定义的 `{DOMAIN}.{REASON}` 大写蛇形；Rust/Go 共享枚举。
- **状态机**：投喂状态机以 04 第 4 节为准，`feed_request_id`（用户幂等）/`device_command_id`（设备幂等）；同一指令必须只出粮一次。
- **时间**：所有持久化与事件均使用 UTC ISO-8601；Rust 用 `time` crate，Go 用 `time.Time`。

## 4. 本次未做（明确 TODO）

- LL-HLS 切片器：仅占位接口，没有真正切片（CDN/边缘节点真实需求需再设计）。
- 转码 worker：未实现 ABR 多档转码，`yunmao-media-edge` 仅做"single profile pass-through"；预留 `transcode_hook` 接口。
- WebRTC SFU：未启动；首期由云直播/CDN/HTTP-FLV 兜底。
- TLS：本机 PoC 全部 HTTP，未引入证书管理；docker-compose 暴露明文端口。
- KMS / Vault：密钥使用环境变量与本地 `secret.yaml`，未对接 KMS。
- Schema Registry：CloudEvents schema 用 JSON Schema 文件存仓内，没起 schema-registry。
- 真正的 sqlc/ent 代码生成：第一版 SQL 由 `pgx` 手写，仓内已放 `go/sqlc.yaml` 模板与 `go/migrations/0001_init.sql`，待 PG 接入后启用。
- 控制面 Go 服务首版仅用内存 store + HTTP 通信，模拟事件总线（feeding-svc → device-edge → gateway 全部 HTTP POST），后续切到 Kafka + PG。

## 5. 决策记录索引

具体 ADR 见 `docs/dev/adr/`：`0001` 记录 monorepo bootstrap，`0002`-`0008` 记录本轮直播协议、事件总线、主数据库、Proto 工具链、投喂安全、WebSocket 鉴权和最小客户端 demo 的架构建议。
