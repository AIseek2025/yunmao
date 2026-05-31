# ADR-0021 支付渠道抽象与 webhook 安全模型

- 状态：Accepted (第七轮)
- 上下文：billing-svc 第六轮已落地 Reserve/Confirm/Cancel saga，但订单实际收款
  仍是 mock；本轮需要接入真实渠道：iOS App Store IAP、Web 微信支付、Web/Wap 支付宝、
  Android 微信/支付宝。各家 SDK、签名、回调格式差异巨大，
  需要在 billing-svc 内做一层统一抽象，避免业务代码与具体 SDK 耦合。

## 决策

### 1. `PayChannel` 接口（单一职责）

```go
type PayChannel interface {
    Name() Channel
    CreatePrepay(ctx, PrepayRequest) (*PrepayResponse, error)
    VerifyWebhook(ctx, raw, headers) (*WebhookEvent, error)
    QueryStatus(ctx, orderID) (*QueryResult, error)
    Refund(ctx, RefundRequest) (*RefundResult, error)
}
```

- `Channel` 枚举：`mock | wechat | alipay | appleiap`。
- `PrepayResponse` 字段对齐：`prepay_id / pay_url / qr_content / client_hints`，
  让客户端无需关心是哪家渠道。
- `WebhookEvent` 字段统一：`order_id / external_trade_no / amount_fen / status`，
  status ∈ `{pending, paid, refunded, closed, failed}`。

### 2. 4 个渠道实现

| Channel | 真实路径 | 当前实现 |
| --- | --- | --- |
| `mock` | n/a | 完整：HMAC-SHA256 签名 + 时间戳窗口 + nonce 防重放 |
| `wechat` | wechatpay-go v3（Native/JSAPI/H5/App + AES-GCM 回调） | 接口骨架；MockMode 用 HMAC-SHA256 等价签名 |
| `alipay` | smartwalle/alipay v3（PC/Wap/App + RSA2 验签） | 接口骨架；MockMode 用 HMAC-SHA256 等价签名 |
| `appleiap` | App Store Server API + Notifications V2 JWS | 接口骨架；MockMode 校验 BundleID + HMAC |

未集成真实 SDK 的原因：本机环境无法访问微信 / 支付宝 / Apple 沙箱；
SDK 引入需要私钥、x5c 证书链，无法在 CI 内伪造。
所有 MockMode 实现保留了「与真实路径相同的字段结构 + 签名/重放语义」，
便于真实接入时仅替换 `CreatePrepay` / `VerifyWebhook` 内部即可。

### 3. webhook 安全模型

- **签名校验**：所有 channel 都强制要求 timestamp + nonce + signature 头；
  缺一不可。mock 通道使用 `HMAC-SHA256(secret, ts+\n+nonce+\n+body)`，
  与微信 v3 的 `SHA256-RSA(public_key, ts+\n+nonce+\n+body)` 同型，
  客户端代码无需改动。
- **时间戳窗口**：5 分钟（与微信 / 支付宝官方一致）；过期请求直接拒绝。
- **重放保护**：channel 实现内置 nonce 缓存（mock/wechat/alipay/appleiap 各 sync.Map）；
  生产路径走 Redis SETNX(nonce, ex=10min)。
- **幂等键**：`(channel, external_trade_no)` 在 `pay_orders` 表上 UNIQUE 约束；
  重复回调直接更新现有行而非新建。
- **Apple IAP 特殊**：除签名校验外，还要校验 `bundle_id == cfg.BundleID`，
  防止跨 app receipt 攻击。

### 4. 表结构（migration 0007）

`pay_orders(id, order_id, channel, prepay_id, external_trade_no, amount_fen,
currency, status, raw_payload, created_at, updated_at, paid_at)`：

- prepay 时 INSERT，status=pending。
- webhook 校验通过后 UPDATE status + paid_at。
- 与 saga 关联：webhook 内调用 `service.MarkPaid(order_id)` 推进 Confirm/Cancel。

### 5. 路由

```
POST /api/v1/orders/{id}/prepay           # X-Pay-Channel 选 channel
POST /api/v1/pay/webhook/{channel}        # 渠道回调
POST /api/v1/orders/{id}/refund/{channel} # 调真实退款
GET  /api/v1/pay/channels                 # 列出已注册 channel（运维 diagnose）
```

### 6. 服务端配置

```
YUNMAO_PAY_MOCK_SECRET=<hex32>
YUNMAO_PAY_WECHAT_ENABLED=true|false
YUNMAO_PAY_WECHAT_MCHID=...
YUNMAO_PAY_WECHAT_APIV3_KEY=...
YUNMAO_PAY_WECHAT_SERIAL_NO=...
YUNMAO_PAY_WECHAT_NOTIFY_URL=...
YUNMAO_PAY_ALIPAY_ENABLED=true|false
YUNMAO_PAY_ALIPAY_APP_ID=...
YUNMAO_PAY_ALIPAY_NOTIFY_URL=...
YUNMAO_PAY_APPLEIAP_ENABLED=true|false
YUNMAO_PAY_APPLEIAP_BUNDLE_ID=live.yunmao.app
YUNMAO_PAY_APPLEIAP_SHARED_SECRET=...
```

凭据缺失时通道自动启用 MockMode（保护性降级），并在启动日志告警。

## 备选与拒绝

- 由各 channel 各自直接接 billing-svc transport：拒绝。
  路径分叉 → 测试矩阵爆炸 → 维护成本不收敛。
- 使用第三方支付聚合（如 Stripe）：拒绝。
  yunmao 国内用户为主，Stripe 不支持微信 / 支付宝直接接入。

## 后果

- 维护成本：每家 SDK 升级独立；接口契约固定，影响范围隔离。
- 风险：MockMode 与生产实现存在分叉；通过单测验证「同型签名」+ E2E 模拟回调减小风险，
  生产上线前要做 4 家渠道的真值联调（见 runbook：`billing-webhook-replay.md`）。
- 测试：单测覆盖 4 channel 的 prepay/webhook/replay/refund；
  transport 层使用 mock channel 跑端到端 prepay → webhook → confirm。
