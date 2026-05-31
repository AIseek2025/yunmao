# Runbook: External Credential Cutover

> 适用：billing-svc 支付渠道（WeChat / Alipay / Apple IAP）+ room-svc TURN 凭据

## Overview

本文档描述从 mock/sandbox 模式切换到生产凭据的完整流程、回滚路径和验证清单。

每个支付渠道和 TURN 服务都支持三级运行模式：

| Mode | 说明 | 触发条件 |
|------|------|----------|
| `mock` | HMAC 等型签名，无真实外部依赖 | `MockMode=true`（CI/dev 默认） |
| `sandbox` | 真实 SDK 签名，指向 sandbox endpoint | `SandboxBase` / `SandboxGateway` 非空 + 凭据齐全 |
| `production` | 真实 SDK 签名，指向生产 endpoint | `MockMode=false` + 凭据齐全 + sandbox 地址为空 |

## Credential Readiness Check

billing-svc 提供 `/internal/diagnose/credentials` 端点，返回每个渠道的凭据就绪状态：

```bash
curl -fsS http://billing-svc:8104/internal/diagnose/credentials | jq .
```

响应示例（mock 模式）：

```json
{
  "wechat/mode": "mock (MockMode=true)",
  "alipay/mode": "mock (MockMode=true)",
  "appleiap/mode": "mock (MockMode=true)"
}
```

也可通过代码调用：

```go
readiness := pay.CheckAllCredentials(wc, ac, ic)
fmt.Println(readiness.Summary())
fmt.Println("AllReady:", readiness.AllReady())
fmt.Println("HasRealMode:", readiness.HasRealMode())
```

## WeChat Pay Cutover

### Required Credentials

| Envvar | Description | Source |
|--------|-------------|--------|
| `YUNMAO_WECHAT_MCH_ID` | 商户号 | 微信支付商户平台 |
| `YUNMAO_WECHAT_APIV3_KEY` | APIv3 密钥（32 字节） | 微信支付商户平台 → API安全 |
| `YUNMAO_WECHAT_SERIAL_NO` | 证书序列号 | 微信支付商户平台 → API证书 |
| `YUNMAO_WECHAT_API_CLIENT_KEY_PATH` | 商户私钥 PEM 路径 | 微信支付商户平台 → API证书 |
| `YUNMAO_WECHAT_PLATFORM_PUBLIC_KEY_PATH` | 平台公钥 PEM 路径（可选） | 微信支付平台证书下载 |
| `YUNMAO_WECHAT_NOTIFY_URL` | 回调通知 URL | 运维配置 |

### Cutover Steps

1. **注入 sandbox 凭据**：

```bash
kubectl -n yunmao set env deploy/billing-svc \
  YUNMAO_ENABLE_WECHAT=true \
  YUNMAO_WECHAT_MCH_ID=<sandbox_mch_id> \
  YUNMAO_WECHAT_APIV3_KEY=<sandbox_v3_key> \
  YUNMAO_WECHAT_SERIAL_NO=<sandbox_serial> \
  YUNMAO_WECHAT_API_CLIENT_KEY_PATH=/run/secrets/wechat-apiclient-key.pem \
  YUNMAO_WECHAT_SANDBOX_BASE=https://api.mch.weixin.qq.com/sandboxnew
```

2. **滚动重启 billing-svc**：

```bash
kubectl -n yunmao rollout restart deploy/billing-svc
kubectl -n yunmao rollout status deploy/billing-svc
```

3. **验证 sandbox 模式**：

```bash
# 检查凭据就绪状态
curl -fsS http://billing-svc:8104/internal/diagnose/credentials | jq .

# 发起一笔 sandbox 支付
curl -X POST http://billing-svc:8104/api/v1/orders/test-order-001/prepay?channel=wechat \
  -H 'Content-Type: application/json' \
  -d '{"amount_fen": 100, "subject": "sandbox test"}'
```

4. **切生产**：移除 `YUNMAO_WECHAT_SANDBOX_BASE`，替换为生产凭据，滚动重启。

### Rollback

```bash
# 回滚到 mock 模式
kubectl -n yunmao set env deploy/billing-svc \
  YUNMAO_ENABLE_WECHAT=false \
  YUNMAO_WECHAT_MCH_ID= \
  YUNMAO_WECHAT_APIV3_KEY= \
  YUNMAO_WECHAT_SERIAL_NO=
kubectl -n yunmao rollout restart deploy/billing-svc
```

## Alipay Cutover

### Required Credentials

| Envvar | Description | Source |
|--------|-------------|--------|
| `YUNMAO_ALIPAY_APP_ID` | 应用 ID | 支付宝开放平台 |
| `YUNMAO_ALIPAY_PRIVATE_KEY_PEM_PATH` | 商户应用私钥 PEM 路径 | 支付宝开放平台 → 应用详情 |
| `YUNMAO_ALIPAY_PUBLIC_KEY_PATH` | 支付宝平台公钥 PEM 路径（可选） | 支付宝开放平台 → 应用详情 |
| `YUNMAO_ALIPAY_NOTIFY_URL` | 回调通知 URL | 运维配置 |

### Cutover Steps

与 WeChat 类似：注入凭据 → 重启 → 验证 sandbox → 切生产。

## Apple IAP Cutover

### Required Credentials

| Envvar | Description | Source |
|--------|-------------|--------|
| `YUNMAO_APPLE_BUNDLE_ID` | Bundle ID（如 `live.yunmao.app`） | App Store Connect |
| `YUNMAO_APPLE_APP_APPLE_ID` | App Apple ID（数字） | App Store Connect（可选） |
| `YUNMAO_APPLE_ROOT_CA_PATH` | Apple Root CA PEM 路径 | Apple PKI 下载 |

### 特殊说明

- Apple IAP 不走 prepay 流程：客户端直接发起 StoreKit2 purchase → JWS 上报 webhook。
- sandbox 验证通过 `SandboxBase` 指向 `https://sandbox.itunes.apple.com`。
- 不需要后端私钥；只需 bundle_id + root CA。

## TURN Cutover

见 `docs/dev/runbooks/turn-credentials-rotation.md`。

## Smoke Checklist

凭据切换后执行以下验证清单：

```bash
#!/bin/bash
# smoke-checklist.sh — 凭据切换后验证

set -euo pipefail

BILLING="http://localhost:8104"
ROOM="http://localhost:8102"

echo "=== 1. billing-svc credential readiness ==="
curl -fsS "$BILLING/internal/diagnose/credentials" | jq . || echo "WARN: diagnose endpoint not available"

echo "=== 2. billing-svc health ==="
curl -fsS "$BILLING/healthz" | jq .

echo "=== 3. room-svc health ==="
curl -fsS "$ROOM/healthz" | jq .

echo "=== 4. mock channel prepay ==="
curl -fsS -X POST "$BILLING/api/v1/orders/smoke-001/prepay?channel=mock" \
  -H 'Content-Type: application/json' \
  -d '{"amount_fen": 100, "subject": "smoke test"}' | jq .

echo "=== 5. wechat channel mode ==="
curl -fsS "$BILLING/api/v1/pay/channels" | jq '.[] | select(.name=="wechat") | {name, mode}' || echo "wechat not registered"

echo "=== 6. alipay channel mode ==="
curl -fsS "$BILLING/api/v1/pay/channels" | jq '.[] | select(.name=="alipay") | {name, mode}' || echo "alipay not registered"

echo "=== 7. appleiap channel mode ==="
curl -fsS "$BILLING/api/v1/pay/channels" | jq '.[] | select(.name=="appleiap") | {name, mode}' || echo "appleiap not registered"

echo "=== 8. TURN credential issuance ==="
curl -fsS "$ROOM/v1/rooms/smoke-room/ice-servers" \
  -H 'Authorization: Bearer test-token' | jq '(.ice_servers // .iceServers // []) | length' || echo "TURN not configured"

echo "=== Smoke checklist complete ==="
```

## Observability

### Key Metrics

| Metric | Type | Alert |
|--------|------|-------|
| `pay_real_mode_active{channel=...}` | gauge | 0 in production = credential issue |
| `yunmao_billing_webhook_invalid_signature_total` | counter | rate > 0.1/s × 5min → Page |
| `coturn_total_allocations_failed` | counter | rate > 5/s × 2min → Page |
| `pay_reconcile_diff_total` | counter | 突增 → 检查渠道状态 |

### Key Logs

```bash
# 支付渠道模式
grep "mode=" /var/log/yunmao/billing-svc.log | jq .

# TURN credential issuance
grep "turn:issue" /var/log/yunmao/room-svc.log | jq .

# Webhook 验签
grep "verify_webhook" /var/log/yunmao/billing-svc.log | jq .
```

## External Blockers

| 依赖 | 状态 | 备注 |
|------|------|------|
| 微信支付 sandbox 凭据 | ⏳ 待业务提供 | 需 mch_id + APIv3Key + 证书 |
| 支付宝 sandbox 凭据 | ⏳ 待业务提供 | 需 app_id + 商户私钥 |
| Apple IAP App Store Connect 配置 | ⏳ 待业务提供 | 需 bundle_id + App Apple ID |
| coturn 生产密钥 | ⏳ 待运维配置 | 见 ADR-0015 KMS 选择与轮换 |
