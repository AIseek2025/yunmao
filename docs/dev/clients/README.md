# yunmao 客户端开发指南（第八轮起）

第八轮起，yunmao 客户端工程拆为四个独立目录：

| 目录 | 平台 | 主要技术栈 | 主要负责人（建议） |
| --- | --- | --- | --- |
| `clients/web/` | Web (PC/H5) | Next.js 15 + Tailwind 4 + TanStack Query | 前端 |
| `clients/admin/` | Admin Web | Next.js 15 + Tailwind 4 | 前端 |
| `clients/ios/` | iOS 16+ | SwiftUI + WebRTC-SDK + StoreKit 2 | iOS |
| `clients/android/` | Android 26+ | Kotlin 2 + Compose + WebRTC | Android |

`clients/web-demo/` 仅作为最小回归 demo 保留，不再接受新功能。

## 共享约定

- 所有端使用同一份 OpenAPI/proto 定义（后续生成到 `pkg/yunmao/openapi/`）。
- 灰度命中算法（`room_id` → 0..99 桶）四端实现必须一致，单测覆盖：
  - Go：`pkg/yunmao/featureflags/hash.go`
  - Rust：`yunmao-webrtc::gray::hash100`
  - Web/Admin：`src/lib/gray.ts`
  - iOS：`Util/GrayHit.swift`
  - Android：`util/GrayHit.kt`
- WS 网关协议参考 `docs/dev/04 deliverable.md`：`auth` → `subscribe` → 业务事件。
- 直播协议自动切换：默认 LL-HLS，灰度命中走 WHEP（`url_whep`），失败回退 HLS。
- 支付：iOS 仅走 Apple IAP；Android 走微信 + 支付宝；Web 走微信 Native（PC 扫码）+ 支付宝 PC。

## 后端入口（开发期）

| 服务 | 端口（默认） | 用途 |
| --- | --- | --- |
| user-svc | 18000 | 登录、JWT 签发 |
| room-svc | 18001 | 房间列表、subscription、ice-servers |
| feeding-svc | 18003 | `POST /v1/feed` |
| billing-svc | 18004 | `prepay`、webhook |
| chat-svc | 18005 | `recent_messages`、moderation |
| admin-svc | 18006 | 运营后台 API |
| gateway WS | 18007 | `wss://.../ws` |

各端 `.env` / `BuildConfig` / `Info.plist` 把这些写为可覆盖配置。

## 端到端联调

参考各目录 README 与 `scripts/turn/`、`scripts/e2e.sh`、`scripts/poc-feed.sh`。
推荐流程：

```bash
make dev-up && make app-up
make migrate-up
make web-dev           # http://localhost:3000
# 另一终端：
make admin-dev         # http://localhost:3100
```

iOS / Android 通过 `10.0.2.2`（Android 模拟器）或 `host.docker.internal`
（iOS 模拟器）连接 host 端服务。
