# yunmao 运营后台（Admin UI）

第八轮（任务 H）落地的运营后台骨架，技术栈与 `clients/web/` 同源：

- Next.js 15（App Router）+ React 19 + TS 5.6（strict）
- Tailwind CSS 4 + 极简灰白配色
- TanStack Query v5 + Zustand
- Vitest 单测 + Playwright E2E（Chromium）

## 页面

| 路径 | 说明 |
| --- | --- |
| `/` | 首页 + 左侧导航 |
| `/feature-flags` | feature-flags-svc CRUD（百分比、enabled） |
| `/feeding-policy` | 投喂策略（冷却、每日上限） |
| `/chat/wordlist` | 弹幕词表 CSV/JSON 批量导入；导入后 admin-svc 广播 `chat.wordlist.updated` |
| `/rooms` | 房间管理（占位，下一轮补全） |
| `/wallet` | 钱包流水（占位） |
| `/webrtc/gray-sim` | WebRTC 灰度模拟器（FNV1a 与后端同源） |

## 鉴权

接 admin-svc 的 RS256 JWT；JWT 需要 `roles=[admin]`。前端把 token 存在
`localStorage.yunmao.admin.token`，由 `adminApi.ts` 统一注入。
登录页（`/login`）尚未实现：第八轮先用浏览器 devtools 注入 token 验证页面；
第九轮接入 user-svc OIDC 流。

## 跑起来

```bash
cd clients/admin
pnpm install
pnpm dev               # http://localhost:3100
pnpm test:run          # vitest
pnpm exec playwright install --with-deps chromium
pnpm e2e               # Playwright
```

## 已知未跑项

- 本工作区未执行 `pnpm install`（无网络/避免 lockfile 污染）。CI 中跑
  `pnpm install --frozen-lockfile && pnpm lint && pnpm test:run && pnpm e2e`。
- `/rooms`、`/wallet` 当前是占位文案；接 admin-svc 后再实现。
