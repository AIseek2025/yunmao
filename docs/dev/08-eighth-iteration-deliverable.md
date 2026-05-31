# yunmao 第八轮迭代交付（2026-05-25）

> 主题：**硬核收尾 + 多端正式工程骨架启动**
>
> 范围：webrtc-rs WHEP Subscriber 闭环、支付/审核真接、TURN 真值脚本、iOS/Android/Web/Admin 四端骨架。
> ADR：0024（webrtc-rs subscriber）、0025（支付真接 + 对账）、0026（审核真接 + 词表热更）。

## 任务对照与完成度

| 优先级 | 任务 | 状态 | 备注 |
| --- | --- | --- | --- |
| A | webrtc-rs WHEP Subscriber 闭环 | ✅ | `cargo build --features webrtc-rs` 通过；in-process forward 单测通过；DTLS/SRTP 物理 loopback 标记 `#[ignore]`，CI（`webrtc-it.yml`）跑 |
| B | 支付真 SDK 真接 + 对账 | ✅ | 微信/支付宝/Apple IAP 三渠道走 stdlib crypto；`GET /api/v1/pay/channels` 暴露 mock/real；reconcile worker + `pay_reconcile_records` 表 |
| C | 阿里云 Green 真接 + 词表热更新 | ✅ | POPv1 HMAC-SHA1 签名走 stdlib；admin-svc 词表 CRUD + 事件广播 + chat-svc 5min 兜底轮询 |
| D | TURN 真值脚本 + e2e | ✅ | `scripts/turn/{turn-up,turn-check}.sh`；`reports/turn/turn-baseline-20260525.md` 含生产清单 |
| E | iOS 骨架 | ✅ | SwiftUI + WebRTC-SDK 125 + StoreKit 2 + KeychainAccess；`swift test` 4/4 通过 |
| F | Android 骨架 | ✅ | Kotlin 2 + Compose + WebRTC-SDK + 微信/支付宝 SDK；本机无 Android SDK，工程结构 + 单测代码就位 |
| G | Web 正式工程化 | ✅ | Next.js 15 + Tailwind 4；`pnpm test:run` 9/9 通过；`pnpm build` 通过 |
| H | Admin 运营 UI | ✅ | Next.js 15；`pnpm test:run` 2/2 通过；`pnpm build` 通过 |

## 新增 / 修改文件清单

### Rust（`rust/`）

- `crates/yunmao-webrtc/src/webrtc_rs.rs`：新增 Subscriber 完整路径（`create_subscriber` + `spawn_forward_task` + `register_lifecycle` + `delete_session`）。
- `crates/yunmao-webrtc/src/whip_whep.rs`：新增 `WebRtcEngine` 枚举（`rs`/`native`）。
- `crates/yunmao-webrtc/src/lib.rs`：导出 `WebRtcEngine`。
- `crates/yunmao-webrtc/Cargo.toml`：增加 `tokio-util`（`CancellationToken`）依赖。
- `.github/workflows/webrtc-it.yml`：dedicated CI 跑 `webrtc-rs-it`。

### Go（`go/services/`）

- `billing-svc/internal/pay/wechat.go`：RSA-SHA256 签名 + AES-GCM 解密路径（真）。
- `billing-svc/internal/pay/alipay.go`：RSA2 签名 / 验签路径（真）。
- `billing-svc/internal/pay/appleiap.go`：StoreKit2 ES256 JWS + x5c 证书链校验。
- `billing-svc/internal/pay/real_paths_test.go`：用本地 RSA/ECDSA 自签证书测试真路径。
- `billing-svc/internal/pay/reconcile.go` + `reconcile_test.go`：对账 worker。
- `billing-svc/internal/transport/http.go`：`GET /api/v1/pay/channels` 暴露 mock/real 模式。
- `chat-svc/internal/moderation/aliyun_green.go` + `aliyun_green_test.go`：阿里云 Green POPv1 签名 + httptest mock 验证。
- `chat-svc/internal/moderation/provider.go`：增加 `Endpoint` 字段。
- `admin-svc/internal/service/service.go` + `service_test.go`：词表 sink + import/list + 版本号。
- `admin-svc/internal/transport/http.go`：词表 PUT/GET/version 三个端点。
- `migrations/0008_pay_reconcile.sql`：对账记录 + 退款订单表。
- `migrations/0009_chat_wordlists.sql`：词表表。

### Scripts / Reports

- `scripts/turn/turn-up.sh`：docker compose 起 coturn，自动注入外部 IP / secret。
- `scripts/turn/turn-check.sh`：`turnutils_uclient` 验证 relay。
- `reports/turn/turn-baseline-20260525.md`：本机/sandbox 基线 + 生产部署清单。

### iOS（`clients/ios/`）

- `clients/ios/YunmaoApp/Package.swift`：SPM 包定义（WebRTC-SDK + KeychainAccess）。
- `Sources/YunmaoApp/App.swift`：SwiftUI App（注：库目标不挂 `@main`）。
- `Sources/YunmaoApp/Models/Models.swift`：DTO（与后端 / Web / Android 同源）。
- `Sources/YunmaoApp/Network/YunmaoAPI.swift`：URLSession async/await + JWT 注入。
- `Sources/YunmaoApp/Network/WSClient.swift`：URLSessionWebSocketTask 事件分发。
- `Sources/YunmaoApp/WebRTC/WhepClient.swift`：WHEP `RTCPeerConnection` recvonly。
- `Sources/YunmaoApp/Payments/StoreKitManager.swift`：StoreKit2 + JWS 上报。
- `Sources/YunmaoApp/Auth/SessionStore.swift`：Keychain token 持久化。
- `Sources/YunmaoApp/Util/GrayHit.swift`：FNV1a 灰度命中（与后端同源）。
- `Sources/YunmaoApp/Views/{Login,RoomList,RoomDetail,Profile}View.swift`：4 个主页面。
- `Tests/YunmaoAppTests/YunmaoAppTests.swift`：4 个单测。
- `clients/ios/README.md`：构建 / Xcode 配置说明。

### Android（`clients/android/`）

- `settings.gradle.kts` / `build.gradle.kts` / `gradle.properties`：根工程。
- `app/build.gradle.kts`：Compose + Ktor + WebRTC-SDK + ExoPlayer + 微信/支付宝。
- `app/src/main/AndroidManifest.xml`：权限 + WXPayEntryActivity 占位。
- `app/src/main/java/live/yunmao/app/`：
  - `YunmaoApplication.kt` / `MainActivity.kt`
  - `model/Models.kt`（DTO）
  - `network/YunmaoApi.kt`（Ktor HTTP）
  - `network/WSClient.kt`（Ktor WS 事件解析）
  - `webrtc/WhepClient.kt`（WHEP）
  - `pay/PayManager.kt`（微信/支付宝拉起）
  - `util/GrayHit.kt`（灰度命中）
  - `ui/`（Compose 页面：Login / RoomList / RoomDetail / Profile）
- `app/src/test/java/live/yunmao/app/`：`GrayHitTest`、`PrepayParseTest`（JUnit4）。
- `app/proguard-rules.pro`：序列化 + WebRTC + SDK keep。
- `clients/android/README.md`：构建说明 + 待办清单。

### Web（`clients/web/`，新建）

- `package.json` / `next.config.ts` / `tsconfig.json` / `tailwind.config.ts` / `postcss.config.mjs`。
- `src/app/layout.tsx` / `page.tsx` / `login/page.tsx` / `rooms/page.tsx` / `rooms/[id]/page.tsx` / `me/page.tsx` / `me/wallet/page.tsx`。
- `src/components/Providers.tsx`（TanStack QueryClient）。
- `src/lib/{types,api,session,gray,ws,playback}.ts`。
- `src/lib/{gray,ws,playback}.test.ts`（Vitest）。
- `vitest.config.ts` / `playwright.config.ts` / `e2e/login.spec.ts`。
- `clients/web/README.md`。
- `clients/web-demo/README.md`：标记 deprecated，指向新工程。
- `docs/dev/clients/{README,web-migration}.md`：客户端工程总览 + 迁移指引。

### Admin（`clients/admin/`，新建）

- `package.json` / `next.config.ts` / `tsconfig.json` / `tailwind.config.ts` / `postcss.config.mjs`。
- `src/app/layout.tsx` / `page.tsx` / `feature-flags/page.tsx` / `feeding-policy/page.tsx` / `chat/wordlist/page.tsx` / `rooms/page.tsx` / `wallet/page.tsx` / `webrtc/gray-sim/page.tsx`。
- `src/components/Providers.tsx`。
- `src/lib/{adminApi,gray}.ts`，`src/lib/gray.test.ts`。
- `vitest.config.ts` / `playwright.config.ts` / `e2e/admin.spec.ts`。
- `clients/admin/README.md`。

### Makefile / 文档

- `Makefile`：新增 `web-dev` / `web-build` / `web-test` / `admin-dev` / `admin-build` / `admin-test` / `android-build` / `ios-build`。
- `docs/dev/adr/0024-webrtc-rs-subscriber.md`、`0025-pay-real-sdk-and-reconcile.md`、`0026-chat-moderation-real-and-wordlist.md`。
- `docs/dev/08-eighth-iteration-deliverable.md`（本文件）。

## 关键能力增量（每项一行）

- webrtc-rs 全链路：`PeerConnection` + `add_track(TrackLocalStaticRTP)` + Hub 订阅 + `CancellationToken` 生命周期。
- 支付真路径：微信 v3（RSA-SHA256 签名 + AES-GCM 解密）、支付宝（RSA2）、Apple IAP（ES256 JWS + x5c）—全部用 Go stdlib，无三方 SDK。
- 对账：billing-svc 周期 worker 拉本地订单 vs channel 状态 → 落 `pay_reconcile_records` + `pay.reconcile.diff` 事件。
- 审核真路径：阿里云 Green POPv1（HMAC-SHA1）；800ms 超时降级到 LocalProvider。
- 词表热更新：admin-svc PUT CSV/JSON → DB → 广播 `chat.wordlist.updated` → chat-svc 缓存刷新（5min 兜底轮询）。
- TURN：脚本化部署 + `turnutils_uclient` 验证 + 生产部署清单。
- 灰度命中：四端 FNV1a 32bit 同源，单测覆盖。
- iOS：SwiftUI + StoreKit2 + WebRTC-SDK + Keychain；4 个单测通过。
- Android：Compose + WebRTC + WeChat/Alipay；Gradle 工程 + 单测代码就位。
- Web：Next.js 15 + Tailwind 4 + TanStack Query + LL-HLS/WHEP 自动切换；9 个单测通过；build 通过。
- Admin：Next.js 15；feature-flags / feeding-policy / wordlist / WebRTC 灰度模拟；build 通过。

## 验证命令与本机跑通结果

| 命令 | 结果 |
| --- | --- |
| `cd rust && cargo fmt --all -- --check` | ✅ 0 错 |
| `cd rust && cargo clippy --workspace --all-targets -- -D warnings` | ✅ 0 错 |
| `cd rust && cargo test --workspace` | ✅ 全部 ok |
| `cd rust && cargo build -p yunmao-webrtc --features webrtc-rs` | ✅ 40s 编译完成 |
| `cd rust && cargo test -p yunmao-webrtc --features webrtc-rs-it -- it_tests::dtls_loopback_60_video_rtp_packets` | ⏸️ 本机缺 cmake / openssl 时跳过，CI（webrtc-it.yml）跑 |
| `make go-vet` | ✅ 9 模块全部通过 |
| `make go-test` | ✅ 所有 pkg/services 单测 ok |
| `cd clients/ios/YunmaoApp && swift test` | ✅ 4/4 测试通过（macOS host 上） |
| `cd clients/ios/YunmaoApp && swift build` | ✅ 库编译成功（含 WebRTC-SDK 125） |
| `cd clients/web && pnpm install && pnpm test:run` | ✅ 9 个测试通过 |
| `cd clients/web && pnpm build` | ✅ 8 个路由 prerender 成功 |
| `cd clients/admin && pnpm install && pnpm test:run` | ✅ 2 个测试通过 |
| `cd clients/admin && pnpm build` | ✅ 10 个路由 prerender 成功 |
| `cd clients/android && ./gradlew assembleDebug` | ⏸️ 本机无 Android SDK / gradle wrapper jar，需要 CI 跑 |

## 仍未完成 / 留 TODO（带原因）

- **webrtc-rs DTLS/SRTP 物理 loopback 测试**：本机 macOS 缺 cmake + openssl 系统依赖，无法稳定跑。CI 上的 `.github/workflows/webrtc-it.yml` 已就位（Ubuntu runner + apt 安装）。
- **Android assembleDebug**：本机无 Android SDK / `gradlew` 二进制（避免提交大型 jar）。CI 中需要 `android-actions/setup-android@v3` + `gradle wrapper --gradle-version 8.7` 自动生成后跑。
- **admin UI 鉴权流**：第八轮 admin 只接 `Bearer` token；登录页与 user-svc OIDC 联动留到第九轮。
- **`/admin/rooms` 与 `/admin/wallet`**：仅落了文案占位，下一轮接 admin-svc API。
- **`web/clients/web-demo` 归档**：暂时保留作为最小回归 demo，第十轮再决定是否搬到 `archive/`。
- **iOS / Android WebRTC 真机回放**：需要 media-edge WHEP endpoint 真值联调，留待 D 任务的 e2e 测试在 CI 上跑通。
- **支付真值联调**：当前仍是 sandbox / mock 凭据；微信、支付宝、App Store Connect 商户证书要业务方提供后才能切 prod。

## 本轮新增 ADR

- **ADR-0024**：webrtc-rs WHEP Subscriber 架构（PeerConnection + TrackLocalStaticRTP + CancellationToken）+ 与自研 packetizer 的共存策略（feature flag `webrtc.engine=rs|native`）。
- **ADR-0025**：支付渠道真接策略 + 凭据管理（`KeyProvider` + env）+ 对账 worker 设计。
- **ADR-0026**：审核 SaaS（阿里云 Green）真接 + 词表热更新事件链路 + 降级策略。

## 仍需用户 / 业务确认

1. 微信支付 v3 商户号、APIv3 密钥、API 证书的 sandbox / prod 切换时间表。
2. 支付宝商户应用 AppID / 私钥的下发流程（KMS / Vault？）。
3. Apple IAP：`Bundle ID = com.yunmao.app.ios` 是否最终值？App Store Connect 内 `com.yunmao.coin.{small,medium,large}` 是否申请？
4. 阿里云 Green AK/SK 的发放与轮转节奏。
5. 微信 Android AppID（`wxabcdef1234567890` 占位）需替换；MD5 签名指纹要联系腾讯开放平台审核。
6. 词表的初始内容（按 region/language 分桶）由谁维护：法务还是运营？
7. coturn 生产部署的外部 IP / TLS 证书 / UDP relay port range 提供方。

## 下一轮建议优先级

1. **A. 媒体侧端到端联调（CI）**：把 `webrtc-it.yml` 跑成绿，含 DTLS/SRTP 60 包断言；接入 `scripts/turn/` 完成 TURN-relay e2e。
2. **B. 支付真值 sandbox 联调**：拿微信 + 支付宝 sandbox 商户号，跑完整 prepay → 扫码 → 回调 → 对账闭环。
3. **C. Admin 鉴权流**：user-svc 加 `roles` claim；admin 登录走 user-svc OIDC + role gate。
4. **D. Android assembleDebug 接 CI**：补 gradle wrapper（运行时生成）+ android-it.yml workflow。
5. **E. 移动端 e2e**：用 `mockttp` / `wiremock` 起 mock 后端，跑 iOS XCUITest + Android Espresso 烟雾测试。
6. **F. OpenAPI 共享 schema**：在 `pkg/yunmao/openapi/v3.json` 暴露后端路由，客户端用 `openapi-typescript` / `openapi-generator` 自动生成 DTO，替换当前手工同源维护。
7. **G. Admin 「房间管理」/「钱包流水」**：补 `/admin/rooms` 与 `/admin/wallet` 真实页面。
