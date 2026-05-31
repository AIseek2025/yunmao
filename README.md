# yunmao 平台底座

云养猫直播投喂平台的底层核心模块（Rust 数据面 + Go 控制面 + 协议契约 + 部署）。

> 详细产品/架构规划见 `docs/finalproductplanning/`，本 README 只覆盖工程上手。

## 目录

```
docs/                  规划与开发日志（00-bootstrap-notes.md 等）
proto/                 共享契约（gRPC + CloudEvents schema + 错误码）
rust/                  Cargo workspace（媒体 ingest / media-edge / gateway / device-edge）
go/                    Go workspace（user/room/feeding/device/billing/admin）
deploy/                docker-compose.dev.yml 起本地基础设施
scripts/               PoC、压测、proto 生成脚本
.github/workflows/     CI 草稿
Makefile               顶层构建/测试入口
```

## 一次启动

```bash
# 0. 工具链（Rust stable 1.80+，Go 1.23+，Docker、ffmpeg 可选）
make dev-up                       # 起 postgres / redis / kafka / minio / jaeger / prometheus / grafana
make rust-build                   # 编译 Rust workspace
make go-build                     # 编译 Go services
```

> 详细的 PoC 流程见 `scripts/poc-feed.sh` 与 `docs/dev/00-bootstrap-notes.md`。

## 端到端 PoC（投喂事件链路 + 直播）

```bash
# 1. 启动基础设施
make dev-up

# 2. 启动 Rust 边缘 + Go 控制面（建议用 tmux 或多个终端）
(cd rust && cargo run --bin yunmao-media-edge) &
(cd rust && cargo run --bin yunmao-gateway) &
(cd rust && YUNMAO_FEED_ACK_URL=http://127.0.0.1:8201/internal/feed-acks cargo run --bin yunmao-device-edge) &
(cd go/services/feeding-svc && go run ./cmd/feeding-svc) &

# 3. 用 ffmpeg 推一路 RTMP 流到 ingest（默认 rtmp://127.0.0.1:1935/live/room_demo）
ffmpeg -re -f lavfi -i testsrc=size=1280x720:rate=30 \
       -f lavfi -i sine=frequency=1000 \
       -c:v libx264 -tune zerolatency -preset veryfast -b:v 2000k \
       -c:a aac -b:a 96k -f flv rtmp://127.0.0.1:1935/live/room_demo

# 4. 浏览器拉流：http://127.0.0.1:8080/live/room_demo.flv（用 mpegts.js / flv.js 测试页）
# 5. 订阅 gateway WebSocket：websocat ws://127.0.0.1:8090/ws
#    输入 {"op":"subscribe","rooms":["room_demo"]}
# 6. 触发投喂：bash scripts/poc-feed.sh
# 7. 观察 gateway WebSocket 收到 feed.command.* 事件
```

## 验证

```bash
cargo fmt --all -- --check      # 在 rust/ 下执行
make lint
make test
bash scripts/gen-proto.sh
```

CI 会等价跑 `cargo fmt --all -- --check`、`cargo clippy --workspace --all-targets -- -D warnings`、`cargo test --workspace`、7 个 Go module 的 `go vet ./...` 与 `go test ./...`，以及 `scripts/gen-proto.sh`。
