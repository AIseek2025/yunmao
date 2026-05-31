# yunmao Web 客户端

第八轮（任务 G）落地的 yunmao 正式 Web 工程，取代 `clients/web-demo/`。

## 技术栈

- Next.js 15（App Router、turbo dev、typed routes）
- TypeScript 5.6（strict）
- Tailwind CSS 4（alpha）+ 内建主题
- TanStack Query v5（数据缓存）+ Zustand 4（轻状态：session）
- LL-HLS：`hls.js`；WebRTC WHEP：`RTCPeerConnection` + `fetch`
- WebSocket：原生 `WebSocket`
- pnpm 9.x + Node 20+
- 单元测试 Vitest 2.x；E2E Playwright 1.48+

## 目录结构

```
clients/web/
├── package.json
├── next.config.ts
├── tsconfig.json
├── tailwind.config.ts
├── postcss.config.mjs
├── vitest.config.ts
├── playwright.config.ts
├── src/
│   ├── app/
│   │   ├── layout.tsx
│   │   ├── page.tsx                 # 落地页
│   │   ├── login/page.tsx
│   │   ├── rooms/page.tsx
│   │   ├── rooms/[id]/page.tsx      # LL-HLS / WHEP 自动切换
│   │   ├── me/page.tsx
│   │   └── me/wallet/page.tsx       # 微信/支付宝充值
│   ├── components/Providers.tsx     # TanStack QueryClient
│   └── lib/
│       ├── api.ts                   # YunmaoApi（统一 JWT）
│       ├── types.ts                 # 共享 DTO
│       ├── ws.ts                    # GatewayWS
│       ├── playback.ts              # LL-HLS / WHEP
│       ├── gray.ts                  # FNV1a 灰度命中（与后端同源）
│       └── session.ts               # Zustand session（localStorage 持久化）
└── e2e/
    └── login.spec.ts                # Playwright 用例
```

## 跑起来

```bash
# 安装
cd clients/web
pnpm install   # 需 Node 20+
# 开发
pnpm dev       # 默认 :3000
# 单测
pnpm test:run
# E2E
pnpm exec playwright install --with-deps chromium
pnpm e2e
```

环境变量（写入 `.env.local` 或注入 CI）：

```bash
NEXT_PUBLIC_API_BASE=http://localhost:18000
NEXT_PUBLIC_WS_BASE=ws://localhost:18007
```

## 直播协议选择

`src/lib/playback.ts::pickProtocol`：

1. 若 `subscription.url_whep` 不存在 → LL-HLS。
2. 若 `webrtc_enabled === false` → LL-HLS。
3. 否则按 `FNV1a(room_id) mod 100 < GRAY_PERCENT` 决定走 WHEP。

`GRAY_PERCENT` 当前硬编码 `50`，与第七轮灰度阶梯 5/20/50/100 一致；后续可改成
通过 `feature-flags-svc` 拉远端配置（admin 后台编辑）。

## 单测覆盖

- `src/lib/gray.test.ts`：FNV1a 一致性 + 5000 个样本分布检测。
- `src/lib/ws.test.ts`：GatewayWS 事件分发解析。
- `src/lib/playback.test.ts`：协议选择 fallback 路径。

## E2E

`e2e/login.spec.ts`：登录页 + 房间页可达。其余飞马场（投喂 / 弹幕）需联调后端，
留到下一轮 e2e 流水线接入（推荐 `make app-up` 后跑）。

## 与 web-demo 的关系

`clients/web-demo/` 标记为 deprecated，仅保留作为最小回归 demo。新功能一律在
`clients/web/` 实现。下一轮可考虑把 `web-demo/` 折叠成一个 Storybook 卡片。

## 已知未跑项（受工具链限制）

- 本工作区未执行 `pnpm install`（无 lockfile，避免污染主仓库锁状态）。CI 中应跑
  `pnpm install --frozen-lockfile && pnpm lint && pnpm test:run && pnpm e2e`。
- `pnpm dlx playwright install chromium` 在 macOS / Linux 上自动下载浏览器。
