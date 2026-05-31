# 从 `clients/web-demo/` 迁移到 `clients/web/`

第八轮（任务 G）落地了 Next.js 15 的正式 Web 工程。
`clients/web-demo/` 仅保留作为最小回归 demo，新功能一律去 `clients/web/`。

## 主要差异

| 维度 | web-demo（v0） | web（v1，本轮） |
| --- | --- | --- |
| 框架 | 纯静态 HTML + JS | Next.js 15 App Router |
| 模块化 | 全部写在 `demo.js` | `src/lib/*`、`src/app/*` 分层 |
| 直播 | flv.js（HTTP-FLV） | hls.js（LL-HLS）+ WHEP 自动切换 |
| 弹幕 | 单消息列表 | 订阅 + recall 撤回 + moderation 状态 |
| 支付 | 仅文案 | 微信 Native + 支付宝 PC 跳转 |
| 测试 | 无 | Vitest 单测 + Playwright E2E |

## 迁移步骤

1. 任何依赖 web-demo `demo.js` 的脚本（CI / e2e）改指向 `clients/web/`。
2. `scripts/e2e.sh` 中调到 web 的部分换成 `pnpm --dir clients/web e2e`。
3. nginx 反代旧路径若有跳转，把 `/web-demo/*` 重定向到 `/`。
4. 移除任何 CDN 引用 `flv.min.js`（web 不再使用 FLV）。
5. 浏览器书签 / 内网地址改成新工程的入口。

## 兼容期

- web-demo 仍保留在仓库内，便于回归对照（特别是 HTTP-FLV 链路）。
- 第十轮起评估是否归档为 `archive/web-demo/`。
