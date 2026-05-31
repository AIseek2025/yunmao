# 第五轮交付记录

> 时间：2026-05-25
> 参与角色：项目总架构师 / 总负责人（兼并 Go + Rust + Ops）
> 沟通语言：中文简体

## 1. 关键交付总览

| ID | 模块 | 状态 | 核心产物 |
|---|---|---|---|
| A | Services 工厂重构 + testcontainers-go e2e 全链路 | ✅ | 6 个 Go svc 抽出 `server/` 工厂；`cat-feeder-sim` 抽 `pkg/simdev`；`go/test/e2e/integration_test.go` 跑投喂+cancel；`.github/workflows/go.yml` 增 integration job |
| B | fMP4 audio sample 写入 + ffmpeg ladder 真转码 | ✅ | `build_media_segment_av` 双 traf + data_offset 回填；`FfmpegLadderWorker`（真 ffmpeg subprocess） + `FakeLadderWorker`（测试无 ffmpeg）；QoE 新字段 |
| C | KMS 真实 SDK 接入 + 设备 MQTT RS256 | ✅ | `pkg/yunmao/authjwt/keyprovider/`（VaultTransit HTTP + AwsKms（含 `-tags kms_aws` 的 aws-sdk-go-v2 适配）；`/internal/keys/health` 三服务端点；ADR-0017；EMQX JWT auth 配置 |
| D | WebRTC WHIP/WHEP MVP | ✅ | `yunmao-webrtc::whip_whep` axum 路由（POST/DELETE）+ `ProtocolPref` + 鉴权钩子；`clients/web-demo` WHEP 播放按钮 |
| E | WS 多实例 baseline + SLO 文档 | ✅ | `bench_ws` 增握手限速 + cold/hot + latency 直方图 + JSON 输出；`docker-compose.bench.yml`；`docs/dev/perf/ws-baseline-v1.md` |
| F | device-svc bridge cancel topic + sim 双路径 | ✅ | `eventbus.TopicFeedCommandCancelledAcked`；`device-svc/internal/bridge` 处理 `cancel_ack`；`pkg/simdev` `--cancel-loss-rate` |
| G | feeding-svc 业务规则补强 | ✅ | `BillingHook` saga（Reserve/Confirm/Cancel）+ `NoopBilling`；按猫每日上限单测覆盖 |

## 2. 新增 / 修改文件（按目录分组）

### docs/

- `docs/dev/adr/0017-device-identity-and-kms-integration.md`（新增）
- `docs/dev/perf/ws-baseline-v1.md`（新增）
- `docs/dev/05-fifth-iteration-deliverable.md`（本文件）

### deploy/

- `deploy/emqx/jwt-authn.conf`（新增，EMQX 5 JWT auth 配置）

### scripts/perf/

- `scripts/perf/docker-compose.bench.yml`（新增，3 网关 + redis，含 sysctls/ulimits）

### Rust（rust/crates/）

- `yunmao-media-edge/src/fmp4.rs`：新增 `AacFrame`、`build_media_segment_av`、`write_moof_av`、`write_mdat_av`、`media_segment_av_has_two_trafs_and_correct_data_offsets` 测试
- `yunmao-media-edge/src/abr.rs`：新增 `FfmpegLadderWorker`（真 ffmpeg subprocess pool）+ `LegMetrics` + `FakeLadderWorker`；旧 `FfmpegSubprocessWorker::ffmpeg_ladder` 标 deprecated
- `yunmao-media-edge/src/qoe.rs`：新增 `abr_publisher_count` / `abr_ladder_bitrate_bps` / `transcode_worker_busy` / `transcode_restarts`
- `yunmao-webrtc/Cargo.toml`：加 axum/tower/hyper 依赖
- `yunmao-webrtc/src/lib.rs`：导出 `whip_whep` 模块
- `yunmao-webrtc/src/whip_whep.rs`（新增）：WHIP / WHEP / 鉴权 / ProtocolPref，含 4 个单测
- `yunmao-gateway/examples/bench_ws.rs`：重写为带握手限速 + 模式 + latency 直方图 + JSON 输出

### Go（go/）

- `pkg/yunmao/authjwt/keyprovider/vault.go`（新增，Vault Transit HTTP，含 approle/token、prehashed sign、JWKS 多版本）
- `pkg/yunmao/authjwt/keyprovider/vault_test.go`（新增，httptest 起 fake Vault 跑 sign+verify）
- `pkg/yunmao/authjwt/keyprovider/awskms.go`（新增，AwsKmsSigner 抽象 + AwsKms KeyProvider）
- `pkg/yunmao/authjwt/keyprovider/awskms_test.go`（新增，mock signer 跑 sign+verify）
- `pkg/yunmao/authjwt/keyprovider/awskms_aws.go`（新增，`//go:build kms_aws` 真 aws-sdk-go-v2 适配）
- `pkg/yunmao/authjwt/signing_remote.go`（新增，`RemoteSigner` interface + 自定义 RS256 SigningMethod）
- `pkg/yunmao/authjwt/jwt.go`：识别 `RemoteSigner` Material，切换到自定义 signing method
- `pkg/yunmao/errors/errors.go`：新增 `PayChannelFailed` 错误码
- `services/user-svc/internal/transport/http.go`、`internal/service/service.go`：新增 `/internal/keys/health`
- `services/room-svc/internal/transport/http.go`、`internal/service/service.go`：新增 `/internal/keys/health`
- `services/device-svc/internal/transport/http.go`、`internal/service/service.go`：新增 `/internal/keys/health`，并让 MQTT 凭证签发兼容 RemoteSigner
- `services/feeding-svc/server/server_test.go`：修正 `SeedRoom` 类型
- `services/feeding-svc/internal/service/service.go` / `billing.go` / `service_test.go`：billing saga + 每日猫上限单测（上一轮收尾，本轮编译修复 + PayChannelFailed 接入）

> 备注：A 与 F、G 主体在上一段对话已落地，本次为编译/测试修复 + 文档/运维补齐。

## 3. 关键模块当前能力（每项一行）

- **Go svc 工厂**：`server.New(deps)` 构建完整 HTTP/gRPC handler + worker；`cmd/main.go` 只组装 deps + 信号处理。
- **e2e 集成**：testcontainers-go 起 PG/Redis/Kafka/EMQX，in-process 起 user/room/feeding/device + cat-feeder-sim，断言 feed/cancel 两路径（timeout 路径骨架就绪，需要 docker 环境跑）。
- **media-edge fMP4**：单 trak + 双 trak（含 audio trun + 多 traf）均能输出，data_offset 单测验证；SPS/AAC parsing 已第四轮落地。
- **media-edge ABR**：`FfmpegLadderWorker` 真起 ffmpeg 子进程（探测 PATH 自动跳过），输出帧通过 mpsc 流式回到上层，含 bytes_in/out、chunks_out、restarts。
- **KMS 后端**：MockKms（本地）+ VaultTransit（HTTP，含 approle）+ AwsKms（含可选 aws-sdk-go-v2 实现）；`/internal/keys/health` 端点暴露 active/kids 信息。
- **device MQTT 凭证**：RS256 JWT + EMQX JWT auth；TTL ≤ 60 分钟；签发兼容 RemoteSigner。
- **WebRTC WHIP/WHEP**：axum 路由 + Content-Type/Authorization 校验 + Location header；房间 `protocol_pref` 字段；mock 签发 SDP answer，真 RTP 链路留 TODO。
- **web-demo**：增加 WHEP 播放按钮（RTCPeerConnection + recvonly transceiver + POST application/sdp）。
- **WS bench**：握手限速、hot/cold 模式、latency 直方图、JSON 输出；docker compose 3 网关编排可一键起。
- **bridge cancel 路径**：`feed.command.cancelled` → MQTT `device/{id}/cmd/cancel`；上行 `cancel_ack` → `feed.command.cancelled_acked`。
- **feeding-svc**：BillingHook saga（Noop 默认；可注入真 billing）；每日按猫上限单测覆盖。

## 4. 验证命令 + 结果

| 命令 | 结果 |
|---|---|
| `cd rust && cargo fmt --all -- --check` | 通过（先 `cargo fmt --all` 应用了 webrtc 文件的格式化） |
| `cd rust && cargo clippy --workspace --all-targets -- -D warnings` | 通过 |
| `cd rust && cargo test --workspace` | 通过（全部 crate；fmp4 6/6、abr 6/6、webrtc 6/6） |
| `make go-vet` | 通过（所有 6 个 svc + pkg + proto） |
| `make go-test` | 通过（authjwt/keyprovider 含 Vault + AwsKms 测试，feeding-svc billing saga + 每日上限测试） |
| `bash scripts/gen-proto.sh && make buf-lint` | 通过 |
| `cd go/test/e2e && go vet -tags=integration ./...` | 通过（仅 CGO 编译告警；测试由 `INTEGRATION=1` 启用，需要 docker） |

> 实测产出 / 限制：
>
> - **Services 工厂启停证据**：`go test ./go/services/feeding-svc/server/...` PASS（`TestServerFactory_CreateFeedRequest`）即证明 server.New + ListenAndServe 工厂启停通畅。
> - **integration 测试**：当前 sandbox 无 docker，无法实际跑容器；GitHub Actions integration job 已就位，需要 docker 环境执行。
> - **ffmpeg ladder 真转码**：`FfmpegLadderWorker::ffmpeg_available()` 探测；如本机有 ffmpeg，`spawn_with_bin` 会真起子进程；测试 `ffmpeg_ladder_spawn_source_only_when_available` 跑 Source leg（始终走 `cat`，不依赖 ffmpeg）。
> - **KMS sign/verify 证据**：`TestVaultTransit_SignAndVerify` 用 httptest 起 fake Vault 真签真验；`TestAwsKms_SignAndVerify` 用 mock signer 真签真验。
> - **WebRTC 信令路径证据**：`whip_then_whep_flow_returns_sdp_answer` 跑 POST /whip + POST /whep，断言 Location header + Content-Type；`auth_required_when_configured` 跑鉴权钩子。
> - **WS baseline**：脚本 + docker compose 就位；实测 10k/50k/100k 数据需要 docker compose 容器 + bench_ws 真跑，sandbox 受限未跑（见文档表格中标注「macOS 单机 / docker」两栏期望值）。
> - **cancel 链路证据**：`go test ./go/services/device-svc/internal/bridge/...` PASS（bridge 测试覆盖 cancel topic）；e2e 测试在 `integration_test.go` 写好 feed/cancel 两路径，等 docker 环境跑。

## 5. 仍未完成的硬核 TODO（带原因）

- **D：真 RTP/SRTP 链路**：本轮只完成 WHIP/WHEP HTTP 信令层，SDP answer 是 stub；真正的 ICE/DTLS/SRTP 接入需要引入 `webrtc-rs` 或 `str0m`，体积大且需要本机能跑 cargo build（含 OpenSSL/NSS）。已在 ADR-0016 + ADR-0017 中保留，下一轮启动。
- **D：发布端复用 publisher pool**：当前 WHIP create_publisher 只创建 `LocalPublisher` stub，没有把上游 H.264/AAC 帧真正喂给 SFU；需要 media-edge 暴露 `PublisherSink` 让 WHEP 端订阅，本轮未做。
- **E：100k 连接真实数据**：sandbox 无 docker，没法跑容器；表格中 50k/100k 是参考预期值，需要客户/灰度环境真跑后回写到 `docs/dev/perf/runs/`。
- **C：localstack KMS 集成测试**：本轮加了 mock signer 测试，但 localstack 容器测试未启动（sandbox 无 docker）；`-tags kms_aws` 的 aws-sdk-go-v2 路径需要在有 docker / 真账号的 CI 上跑。
- **HS256 兼容下线**：本轮 device-svc MQTT 凭证已支持 RS256 + RemoteSigner，但 HS256 转开关仍保留（避免破坏既有 PoC 链路），ADR-0014 计划 6 轮下线。
- **B：媒体边缘的 audio mdat 端到端串接**：fMP4 `build_media_segment_av` 已就位，但 `ll_hls::Packager` / `Publisher` 调用面尚未改为传入 `AacFrame` 列表；目前仍以 video-only 走 LL-HLS（hls.js 自动嗅探后并入音轨），下一轮接入。

## 6. 本轮新增临时决策

- **ADR-0017**：设备身份与 KMS 集成。把 device MQTT 凭证切到 RS256 + RemoteSigner（Vault/AWS KMS 委托签名）；规定 kid 命名（`transit:<key>:v<n>`、`awskms:<keyid>`）、健康检查端点 `/internal/keys/health` 输出格式、EMQX JWT 插件配置。
- **WebRTC 信令路由**：复用 room subscription token 作为 WHIP/WHEP Authorization Bearer；`protocol_pref=ll-hls|webrtc` 落到房间元数据（room-svc 持久化逻辑下一轮接入）。
- **WS 扩容公式**：单实例 `C_max ≈ 80k`（中等业务），总实例数 `ceil(N × 1.3 / C_max)`；房间订阅表内存上限 `per_instance_rooms ≤ 50,000`。

## 7. 仍需用户 / 业务确认的问题

1. **billing-svc 真实形态**：本轮 `BillingHook` 仅占位，是否需要把它接到 `billing-svc` 的 `Charge` / `Refund` HTTP API（saga 编排还是事件驱动）？现有 mock `NoopBilling`，业务上线前需明确。
2. **每日上限默认值**：当前 `feedingsafety.DefaultGlobal.CatDailyLimit` 是开发兜底，正式数值需业务方给出（产品要求“健康约束”但未给具体 g 数）。
3. **Vault / AWS KMS 选型**：本轮两条路径都已就绪；生产首选哪个？影响运维 SRE 的 oncall 流程与故障演练剧本。
4. **WHIP 推流来源**：直播主播端是否走 WHIP？还是仍由 RTMP 推流 + 平台内部桥接到 WebRTC？影响 D 下一轮架构。
5. **protocol_pref 推荐策略**：默认 LL-HLS 还是 WebRTC？客户端协议降级链如何编排？

## 8. 下一轮建议优先级（草案）

1. **D 收尾（高）**：接入 `webrtc-rs` 或 `str0m`，真 RTP/SRTP 链路；从 media-edge publisher 取 H.264/AAC 流，按 WHEP 发；和 WHIP 上行联调（OBS WebRTC plugin）。
2. **C 收尾（高）**：HS256 兼容路径正式下线，所有服务统一走 RS256 + KeyProvider；ADR-0014 状态从 Accepted 切到 Implemented；EMQX 容器与 device-svc 集成 e2e。
3. **B 收尾（中）**：把 `AacFrame` 接入 `ll_hls::Packager`；LL-HLS master playlist 自动按可用 ladder 输出；media-edge 跑通真 ffmpeg ladder 端到端（CI 上要装 ffmpeg）。
4. **A+E 真实数据（中）**：在有 docker 的 CI / 灰度环境上跑 integration 测试 + WS 50k/100k baseline，回写真实数据到 deliverable / perf md。
5. **G 联动 billing-svc（低）**：BillingHook 接入真 billing-svc 流程；定义 saga 失败补偿事件链路。
6. **运维侧**：把 `/internal/keys/health` 列入 Grafana / 黑盒巡检；EMQX JWT 配置纳入 dev-up；feeding-svc cat-daily-limit metric 加 alert。
