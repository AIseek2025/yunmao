# Runbook: billing 回调（webhook）重放 / 异常处置

> 适用：billing-svc Webhook (`/api/v1/pay/webhook/{channel}`)；channel ∈ {`wechat`, `alipay`, `appleiap`, `mock`}

## 设计回顾

- 所有 channel 走 `PayChannel.VerifyWebhook(ctx, raw, headers) → (PaidEvent, err)`：
  - WeChat：v3 APIv3 AES-GCM 解密 + 时间戳 5min 限制。
  - Alipay：RSA2 验签 + 平台公钥。
  - AppleIAP：StoreKit notification JWS (`x5c` 链至 Apple Root) + bundle ID 校验。
  - Mock：HMAC-SHA256(`raw`, secret) 校验。
- 幂等键：`(channel, external_trade_no)` 唯一约束在 `pay_orders` 表；重复回调直接返回 200。

## 触发场景

1. 渠道侧重试同一 webhook（正常）。
2. 攻击者重放历史 webhook（异常）。
3. 我方处理失败（500/数据库挂）渠道侧重试（半正常）。

## 处置流程

### 排查重放原因

```bash
# 1. 查看 pay_orders 表当前状态
psql $YUNMAO_PG_DSN -c "
  SELECT id, channel, external_trade_no, status, paid_at, created_at, updated_at
  FROM pay_orders
  WHERE channel = '<channel>' AND external_trade_no = '<trade_no>';
"

# 2. 查看 webhook 入参日志（结构化日志 trace_id 追踪）
grep "channel=<channel>" /var/log/yunmao/billing-svc.log | jq 'select(.trade_no=="<trade_no>")'
```

### 重放判定矩阵

| 情况 | 现象 | 处置 |
| --- | --- | --- |
| 正常重试 | `status=paid`，重复 200 OK | 无需介入 |
| 时间戳过期（攻击） | `status=pending`，签名通过但 `WeChat-Pay-Timestamp` 偏差 > 5min | webhook 拒绝；运维查 access log + IP，加入 SLB 黑名单 |
| 签名非法 | log 报 `signature mismatch` | 拒绝 + 报警；查渠道密钥/证书是否泄露 |
| 我方处理失败 | `status=pending` 持续 > 5min，但渠道返回支付成功 | 走手工 confirm 流程（见下方） |

### 手工补 confirm

```bash
# 1. 校对：从渠道侧 console 拉取实际状态。
# WeChat: https://pay.weixin.qq.com/index.php/core/order/index
# Alipay: https://b.alipay.com/order/order.htm

# 2. 调 billing-svc 内部接口：使其 saga 走 Confirm。
curl -X POST 'http://billing-svc:8500/api/v1/orders/<order_id>/confirm-manual' \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Operator: ops@yunmao' \
  -d '{"reason":"webhook replay missed", "external_trade_no":"<trade_no>"}'
# 该接口仅在内网路由暴露；走 mTLS。
```

### 防重放加固

- WeChat：v3 SDK 已内置 `Wechatpay-Nonce` 校验 + 时间戳。
- Alipay：`sign_type=RSA2` + 时间戳；自实现 nonce 校验入 Redis 30min。
- AppleIAP：JWS `iat` 字段 + `originalTransactionId` 全局唯一。
- Mock：在 CI 测试中已覆盖重放保护用例（见 `pay/mock_test.go::TestVerifyWebhookRejectsReplay`）。

## 报警阈值

- `yunmao_billing_webhook_invalid_signature_total` rate > 0.1/s 持续 5min → Page。
- `yunmao_billing_webhook_replayed_total{outcome="rejected"}` 突增 → 警告。
- `pay_orders.status=pending` 超过 10min 数量 > 10 → 警告。
