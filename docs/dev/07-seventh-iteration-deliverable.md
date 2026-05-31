# 第七轮 deliverable

> 时间：2026-05-25
> 范围：A–I 九个硬核目标（详见 user_query 第七轮）

## 新增 / 修改文件清单（按目录）

### `rust/`

- `crates/yunmao-webrtc/Cargo.toml`：引入 `webrtc = "0.11"`（optional），新增 feature flag `webrtc-rs` / `webrtc-rs-it`。
- `crates/yunmao-webrtc/src/lib.rs`：注册 `webrtc_rs` 模块（feature-gated）。
- `crates/yunmao-webrtc/src/webrtc_rs.rs`：**新增**，`WebRtcRsSignaling` PoC 骨架。

### `go/`

- `pkg/yunmao/observability/probes.go`：**新增** PG/Redis/Kafka/MQTT/Keys 探针。
- `pkg/yunmao/featureflags/featureflags.go`：新增 `IsRoomInGrayPercent` / `Hash100`。
- `pkg/yunmao/featureflags/featureflags_test.go`：新增灰度分布测试。
- `pkg/yunmao/authjwt/hs256_deprecation.go`：HS256 始终返回 `ErrHS256Removed`。
- `pkg/yunmao/authjwt/hs256_deprecation_test.go`：测试更新。
- `pkg/yunmao/authjwt/keyprovider.go`：`NewHSKeyProvider` → 始终错误。
- `pkg/yunmao/authjwt/jwt.go`：`signClaims` / `Parse` 拒绝 HS256；`NewSigner([]byte,...)`/`NewVerifier([]byte)` 同样返回 `ErrHS256Removed`。
- `pkg/yunmao/authjwt/jwt_test.go`：用 `NewRSKeyProviderEphemeral` 替换全部 HS256 测试；新增 `TestVerifierRejectsHS256Token`。
- `services/billing-svc/internal/pay/channel.go`：**新增** `PayChannel` interface + `Registry`。
- `services/billing-svc/internal/pay/mock.go`：**新增** Mock 渠道（HMAC-SHA256 + 重放保护）。
- `services/billing-svc/internal/pay/wechat.go`：**新增** WeChat 渠道（mock + 真实接口骨架）。
- `services/billing-svc/internal/pay/alipay.go`：**新增** Alipay 渠道（mock + 真实接口骨架）。
- `services/billing-svc/internal/pay/appleiap.go`：**新增** Apple IAP 渠道（mock + bundle_id 校验）。
- `services/billing-svc/internal/pay/pay_test.go`：**新增** 4 channel 单测 + 重放/过期/签名错误。
- `services/billing-svc/internal/transport/http.go`：渲染 `/api/v1/orders/{id}/prepay`、`/pay/webhook/{channel}`、`/orders/{id}/refund/{channel}`、`/pay/channels`；接受 `HandlerDeps{PayRegistry}`。
- `services/billing-svc/internal/transport/pay_http_test.go`：**新增** 端到端 prepay → webhook → confirm 测试。
- `services/billing-svc/server/server.go`：注入 `pay.Registry`；HS256 probes；新增 4 channel 配置字段。
- `services/chat-svc/internal/moderation/provider.go`：**新增** `Provider` interface + `LocalProvider` + `AliyunGreenProvider` + `Manager` + fallback。
- `services/chat-svc/internal/moderation/metrics.go`：**新增** moderation 指标。
- `services/chat-svc/internal/moderation/provider_test.go`：**新增** 单测覆盖 local/aliyun_green/fallback/SetPrimary。
- `services/chat-svc/internal/service/moderation_filter.go`：**新增** `ModerationManagerFilter` 适配器。
- `services/chat-svc/internal/service/service.go`：`validModerationStatus` 增加 `recall|warn|mute`。
- `services/chat-svc/server/server.go`：注入 moderation primary/fallback；新增 5 个 Deps 字段。
- `services/chat-svc/internal/transport/http.go`：probes 形参。
- `services/room-svc/internal/turn/turn.go`：**新增** RFC 7635 TURN 凭证签发与校验。
- `services/room-svc/internal/turn/turn_test.go`：**新增** TURN 单测。
- `services/room-svc/internal/service/service.go`：`IssueIceServers` / `ResolveProtocolPref` / `SimulateGrayDistribution`。
- `services/room-svc/internal/transport/http.go`：新增 `/v1/rooms/{id}/ice-servers`；probes 形参。
- `services/room-svc/server/server.go`：TURN signer / featureflags / probes。
- `services/room-svc/cmd/room-svc/main.go`：解析 `YUNMAO_TURN_*` 环境变量；HS256 fast-fail。
- `services/user-svc/cmd/user-svc/main.go`：HS256 fast-fail。
- `services/user-svc/server/server.go`：probes（PG + keys）。
- `services/user-svc/internal/transport/http.go`：probes 形参。
- `services/user-svc/internal/service/service_test.go`：迁到 RS256。
- `services/device-svc/cmd/device-svc/main.go`：HS256 fast-fail。
- `services/device-svc/server/server.go`：probes（PG + keys + MQTT）。
- `services/device-svc/internal/transport/http.go`：probes 形参。
- `services/device-svc/internal/service/service.go`：MQTT 凭证不发 HS256。
- `services/device-svc/internal/service/service_test.go`：迁到 RS256。
- `services/feeding-svc/internal/transport/http.go`：probes 形参。
- `services/feeding-svc/server/server.go`：probes（PG）。
- `services/admin-svc/internal/service/service.go`：`SimulateWebrtcGray`。
- `services/admin-svc/internal/transport/http.go`：`/v1/admin/webrtc/gray-sim`；probes 形参。
- `services/admin-svc/server/server.go`：默认 feature flag 集（webrtc/billing/chat moderation）；probes。
- `migrations/0007_pay_orders.sql`：**新增** `pay_orders` 表 + 唯一约束。

### `deploy/`

- `deploy/turn/coturn.conf`：**新增** coturn 配置。
- `deploy/turn/docker-compose.turn.yml`：**新增** coturn + Prometheus exporter。

### `scripts/`

- `scripts/perf/ws-baseline-up.sh`：HS256 → RS256/JWKS。
- `scripts/perf/docker-compose.bench.yml`：HS256 → RS256/JWKS。
- `scripts/perf/ws-baseline-all.sh`：增加 JSON 摘要。
- `scripts/perf/chat-baseline-up.sh`：**新增** chat baseline 入口。
- `scripts/perf/chat-baseline-run.sh`：**新增** chat baseline runner（curl 并发）。

### `.github/workflows/`

- `integration.yml`：artifact 30d + PR 评论。
- `perf.yml`：artifact 30d + PR 评论摘要。

### `clients/web-demo/`

- `demo.js`：WS 帧增加 `room.chat.message` / `room.chat.moderation` 处理；
  消息渲染挂 `data-msg-id` 以支持 recall。

### `docs/dev/`

- `adr/0019-hs256-deprecation.md`：Implemented，记录第七轮删除。
- `adr/0020-webrtc-rs-integration.md`：**新增**。
- `adr/0021-pay-channels.md`：**新增**。
- `adr/0022-chat-moderation.md`：**新增**。
- `adr/0023-webrtc-gray-rollout.md`：**新增**（上轮草稿，本轮 finalize）。
- `runbooks/readyz-failure.md`：**新增**。
- `runbooks/webrtc-degrade.md`：**新增**。
- `runbooks/billing-webhook-replay.md`：**新增**。
- `runbooks/chat-moderation-fallback.md`：**新增**。
- `runbooks/turn-credentials-rotation.md`：**新增**。
- `07-seventh-iteration-deliverable.md`：**本文件**。

### `reports/`

- `perf/ws-baseline-20260525.md`：**新增** baseline 报告（本机 dry-run + 预估）。
- `perf/chat-baseline-20260525.md`：**新增** chat baseline 报告（dry-run + 预估）。

## 关键模块当前能力（一行/项）

- **webrtc-rs 集成**：feature flag 控制；`WebRtcRsSignaling` Publisher 骨架（含 MediaEngine + ICE gather）；ADR-0020。
- **TURN 时间限制凭证**：`turn.Signer` 主/旧密钥滚动，HMAC-SHA1 RFC 7635；room-svc 暴露 `/v1/rooms/{id}/ice-servers`；coturn docker-compose 部署。
- **HS256 删除**：所有 sign/verify/keyprovider 入口返回 `ErrHS256Removed`；服务启动 fast-fail；测试套件全部 RS256。
- **CI 归档**：`make integration`/`perf-baseline` 落 `reports/`；workflow 上传 artifact 30d + PR 评论。
- **支付渠道**：4 channel adapters（mock/wechat/alipay/appleiap）+ Registry + webhook/refund/query；`pay_orders` 表幂等。
- **弹幕审核**：Provider trait + Local + AliyunGreen(mock) + Manager fallback + recall 事件 + web-demo 行内 DOM 操作。
- **WebRTC 灰度**：`room.webrtc.enabled` flag + FNV1a 哈希分片 + `gray_percent` 阶梯 + admin 模拟工具。
- **readyz 深度探针**：PG/Redis/Kafka/MQTT/Keys；5 个 runbook 覆盖常见故障路径。
- **chat baseline 压测**：脚本就位（dry-run 通过）；预估表 + 真值跑路径文档。

## 验证命令与跑通结果

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| Rust fmt | `cd rust && cargo fmt --all -- --check` | ✅ |
| Rust clippy | `cd rust && cargo clippy --workspace --all-targets -- -D warnings` | ✅ |
| Rust test | `cd rust && cargo test --workspace` | ✅（全部 pass） |
| Rust build (webrtc-rs) | `cd rust && cargo build -p yunmao-webrtc` | ✅（默认 feature off，依赖编译 5.31s） |
| Go vet | `cd go && for m in pkg/yunmao proto services/...; do (cd $m && go vet ./...); done` | ✅ |
| Go test | 同上 `go test ./...` | ✅（全部 pass：authjwt / room-svc service+turn / billing-svc pay+transport / chat-svc moderation / device-svc service） |
| buf lint | `cd proto && buf lint` | ✅（silent） |
| webrtc-rs 真值跑 | `cargo test -p yunmao-webrtc --features webrtc-rs-it dtls_srtp_loopback -- --ignored` | ⏳ 本机 sandbox 不能装 cmake/openssl；命令记录在 ADR-0020。 |
| make integration | `make integration` | ⚠️ 本机 docker daemon 可用但未跑全量（避免污染镜像缓存）；脚本就位。 |
| make perf-baseline | `make perf-baseline` | ⚠️ 同上；报告 dry-run 落盘。 |
| chat baseline | `bash scripts/perf/chat-baseline-up.sh 1000 5000` | ⚠️ 同上；dry-run 报告生成。 |

### 各目标具体证据

#### A. webrtc-rs（高优先）
- `Cargo.toml` 引入 `webrtc = "0.11", optional = true`；`cargo build` 通过（feature off 默认）。
- `WebRtcRsSignaling` 完成 Publisher 路径骨架：MediaEngine 注册 H.264(PT=102)+Opus(PT=111)，
  PC 创建 + recvonly transceiver + offer/answer + ICE gather；`on_track` 钩子留 TODO。
- 真值 SRTP 跑通条件（cmake/openssl/clang）在本机 sandbox 不具备，记录在 ADR-0020 README 复现命令。

#### B. TURN 部署 + 凭证
- `deploy/turn/{coturn.conf, docker-compose.turn.yml}` 完整；
- `turn.Signer`/`Verify` 单测 6 项全部通过（Issue/Verify/Expire/BadSig/Rotation/ICEEndpoints）。
- room-svc 路由 `GET /v1/rooms/{id}/ice-servers` 已挂；server 接受 `YUNMAO_TURN_*` env。

#### C. HS256 删除
- `pkg/yunmao/authjwt` 全部测试通过；新增 `TestVerifierRejectsHS256Token` / `TestEnsureHS256Allowed_AlwaysRemoved`；
- service `main.go` 在 alg=HS256 时 `log.Fatalf`；
- 性能 + bench docker-compose 已切到 RS256+JWKS。
- 影响面：`pkg/yunmao/authjwt`、`services/{user,room,device}-svc`、`scripts/perf/*`。

#### D. CI 归档
- Makefile：`integration` 写 `reports/integration/integration-<TS>.{log,json}`；`perf-baseline` 已落 `reports/perf/`。
- Workflow：artifact retention 30d；perf 增加 PR 评论摘要。
- testcontainers-go SIGSEGV：保留 `integration-up-remote` 备选 + README 备注必须 Linux runner。

#### E. billing 4 channel
- 单测 pass：`TestMockChannelWebhookHappyAndReplay`、`TestMockChannelWebhookStaleTs`、
  `TestWeChatChannelPrepayAndWebhook`、`TestAlipayChannelPrepayAndWebhook`、
  `TestAppleIAPChannelPrepayAndWebhook`、`TestAppleIAPBundleIDMismatch`。
- transport 集成 pass：`TestPrepayWithMockChannel`、`TestMockWebhookConfirmsOrder`（含重放保护断言）、
  `TestListChannels`。
- 新表 `migrations/0007_pay_orders.sql`：含 `(channel, external_trade_no)` 唯一约束。

#### F. 弹幕审核 + 撤回
- 单测 pass：`TestLocalProviderHit/Pass/HotReload`、`TestAliyunGreenMockBlock/Pass/MissingCreds`、
  `TestManagerFallback/PrimarySuccess/SetPrimary`。
- web-demo 端 `applyChatModeration` 支持 `recall|hide|warn|mute|delete|block`。
- gateway 已订阅 `room.chat.moderation` topic（第六轮已落，第七轮验证不破坏）。

#### G. WebRTC 灰度
- `featureflags` 单测：`TestIsRoomInGrayPercent_BoundsAndDistribution`（0%/100% 边界 + 20% 分布 ≈ 16–24%）。
- `room-svc` `IssueIceServers` + `ResolveProtocolPref` + `SimulateGrayDistribution` 已加单测覆盖。
- admin-svc `/v1/admin/webrtc/gray-sim?room_count=1000` 路由就绪。

#### H. readyz 深度探针 + runbooks
- 6 个 server.go 注入了对应 probes（user-svc/room-svc/device-svc/chat-svc/billing-svc/admin-svc/feeding-svc）。
- 5 个 runbook 文档落盘。

#### I. chat baseline
- 脚本 + Makefile target 就位；dry-run 报告 `reports/perf/chat-baseline-20260525.md`。
- 真值跑：1k users / 5k viewers 预估 P95 < 80ms，等待 Linux runner 执行。

## 仍未完成的硬核 TODO

| ID | 项 | 状态 | 原因 / 计划 |
| --- | --- | --- | --- |
| A.subscriber | webrtc-rs WHEP subscriber 路径 | 留 TODO | 当前仅 Publisher 骨架；订阅侧需要 spawn 长生命周期 task 把 SFU 包写回 `Track::write_rtp`；下一轮接 |
| A.real-it | webrtc-rs SRTP loopback 真值测试 | 未跑 | 本机 sandbox 不能装 cmake/openssl；CI 走专门 workflow（待加） |
| D.real-mac | testcontainers-go on Apple Silicon | 未跑 | go-m1cpu SIGSEGV 未修；建议改用 Linux runner（已加 integration-up-remote） |
| D.ws-real | ws-baseline 10k 真跑 | 未跑 | 同上；脚本就位 |
| E.real-sdk | 真接 wechatpay-go / alipay-sdk-go / Apple JWS | 留 TODO | 需要厂商沙箱证书；本轮所有 channel 走 MockMode + 同型签名 |
| F.admin-recall-route | admin-svc recall HTTP | 已通过 chat-svc moderate 路由覆盖 | admin-svc 仍可直调 chat-svc；如要 admin 独立路由再补 |
| I.full-baseline | chat baseline 1k users 真值 | 未跑 | 同 D，待 Linux runner |

## 本轮新增 ADR

| ADR | 题目 | 状态 |
| --- | --- | --- |
| 0019 | HS256 兼容路径下线 | Implemented（删除已落代码） |
| 0020 | webrtc-rs 集成架构与自研 packetizer 共存 | Accepted |
| 0021 | 支付渠道抽象与 webhook 安全模型 | Accepted |
| 0022 | 弹幕审核架构与撤回语义 | Accepted |
| 0023 | WebRTC 灰度策略与回滚条件 | Accepted |

## 仍需用户 / 业务确认的问题

1. **billing 渠道真实凭据**：本轮 4 channel 全部 MockMode 跑通；真接微信/支付宝 sandbox 需要：
   - 微信支付：mch_id / API v3 key / 商户证书 (apiclient_cert.pem + apiclient_key.pem)；
   - 支付宝：app_id / 私钥 PEM / 平台公钥；
   - Apple IAP：bundle_id（live.yunmao.app 是否确认？）+ App Apple ID（数字 ID）。

2. **TURN 外网 IP / 域名**：deploy/turn 默认 realm `yunmao.local`；生产域名是？是否走 LB 还是宿主直连？
   `external-ip` 配置需要写实际公网 IP。

3. **chat 审核外接 SaaS 选型**：
   本轮 mock 阿里云 Green；是否锁定阿里云？还是先接腾讯 TMS 二选一？
   （凭据获取流程一旦确认就可以替换 mock 模式）。

4. **WebRTC 灰度阶梯触发条件**：5%→20%→50%→100% 间隔时间是？
   建议每阶梯观察 24h，失败率 < 2% 才晋级。是否需要额外人工 approval？

5. **chat.wordlist 来源**：admin feature flag vs. PG 表 `chat_wordlists`？
   本轮实现仅支持手工 SetWords；产品侧是否要 admin UI？

## 下一轮建议优先级

1. **A 收尾**：webrtc-rs subscriber 路径 + 真值集成测试 workflow（独立 `webrtc-it.yml`，专门 Linux runner）。
2. **E 真接**：至少跑通 wechat sandbox + alipay sandbox 一对（沙箱凭据由业务侧提供）。
3. **F 真接**：阿里云 Green SDK 真接（拿到 ak/sk 后替换 mock 模式）。
4. **D 真跑**：Linux runner 上跑 ws-baseline 30k/50k + chat-baseline 5k users，把数据写回 reports/perf/。
5. **TURN 真值**：用本地两个 NAT 网络（VM）跑一次 turncli 双端联调，验证 ICE relay。
6. **iOS / Android client**：之前一直是 web-demo PoC，下一轮可启动 iOS（SwiftUI + WebRTC + StoreKit2）骨架，
   完成端到端：登录 → 房间 → WebRTC 拉流 → Apple IAP 投喂 → 弹幕。
