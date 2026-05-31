# ADR-0025：支付渠道真接（WeChat / Alipay / Apple IAP）+ 对账机制

- 状态：Accepted（2026-05-25，第八轮 B 落地）
- 关联：ADR-0021（支付渠道抽象与 webhook 安全模型）

## 背景

第七轮我们落了 PayChannel 抽象 + 4 个 channel（mock/wechat/alipay/appleiap），但所有
真实签名都是 HMAC-SHA256 等型 mock。本轮目标是接入生产级签名与对账机制：
- WeChat：v3 RSA-SHA256 + AES-GCM 资源解密；
- Alipay：RSA2（PKCS1v15 + SHA256）+ form-encoded notify 验签；
- Apple IAP：StoreKit2 JWS（ES256 + x5c chain）+ bundle_id / appAppleId 校验；
- 对账：每小时跑一次对账 worker，把 diff 写 `pay_reconcile_records` 并发
  `pay.reconcile.diff` 事件。

## 决策

### 1. 真接策略：stdlib 优先，避免重 SDK 依赖

不引入 `github.com/wechatpay-apiv3/wechatpay-go` / `smartwalle/alipay` /
`awa/go-iap`，原因：
- 这三个 SDK 实质都是 stdlib `crypto/{rsa,aes,ecdsa,x509}` 的封装；
- 监管 / 安全审计要求对支付签名链路全栈可审，自实现更可控；
- 减少传递性依赖（SDK 们带 protobuf / grpc / yaml 等）；
- 仍保留升级到官方 SDK 的开关位（MockMode → false 后由 server.go 注入真实 HTTP client）。

实现位置：
- `services/billing-svc/internal/pay/wechat.go`：`realCreatePrepay` / `VerifyWebhook`（含 AES-GCM）；
- `services/billing-svc/internal/pay/alipay.go`：`realCreatePrepay` / `alipayRSA2Sign` / `alipayVerify`；
- `services/billing-svc/internal/pay/appleiap.go`：`verifyJWS` + `decodeAndVerifyAppleJWS`。

### 2. MockMode 自动降级

每个 channel 都有 `MockMode bool`。逻辑：
- 显式 `MockMode=true` → 走 HMAC 等型签名（CI / dev 默认）；
- 显式 `false` 但凭据缺失 → 自动改为 `MockMode=true`（避免启动失败 + 安全降级）；
- 显式 `false` 且凭据齐全 → 走真接路径（生产）。

`IsRealMode()` 暴露给 `/pay/channels` 端点 + 监控。

### 3. 配置注入

`Deps.WeChatConfig` / `Deps.AlipayConfig` / `Deps.AppleIAPConfig` 在 `server.New(...)`
注入：
- 关键证书 / 私钥通过 KeyProvider（ADR-0015 KMS）从 env / k8s Secret 加载；
- 单机 dev 模式下 `MockMode=true`。

### 4. 对账 worker

`pay.ReconcileWorker`：
- 周期：1 小时（生产可调，admin feature flag `billing.reconcile.interval_minutes`）；
- 数据源：`LocalOrderSource` interface（由 service 注入 PG 适配器，dev 默认 in-memory）；
- 比对：每订单 `Channel.QueryStatus(orderID)` → diff = status/amount 不一致；
- 落库：`pay_reconcile_records`（迁移 0008）；
- 事件：`pay.reconcile.diff`（key=order_id）→ admin 监控 + alert。

### 5. 退款单据

`pay_refund_orders`（迁移 0008）：
- `refund_id` 由渠道返回，`raw_response` 存原始 SDK 响应；
- 状态：ok / pending / failed；
- Apple IAP 的 refund 始终 pending（Apple 端异步发起）。

### 6. 测试策略

- 单测：`real_paths_test.go` 用 stdlib 在测试中现场生成 RSA / ECDSA keypair
  + 自签证书 → 构造真实签名 / AES-GCM ciphertext / JWS → 验证 channel
  能正确解码 + 校验。
- 集成测试：CI 在 webhook → mark_paid 链路上 mock 真实签名头；测试服务
  能用真接路径完成 confirm。
- 对账 worker：`reconcile_test.go` 用 mock channel 模拟 diff 触发。

### 7. 凭据来源（业务待定）

- 微信支付：mch_id / api_v3_key / serial_no / apiclient_cert.pem / apiclient_key.pem / platform_public_key.pem；
- 支付宝：app_id / 商户应用私钥 PEM / 支付宝平台公钥 PEM；
- Apple IAP：bundle_id (live.yunmao.app) / app_apple_id（待业务确认）；
- TrustedRoots：Apple Root CA PEM（验证 leaf cert chain，建议加入）。

业务侧需要在 7 月初前提供 sandbox 凭据用于联调。

## 影响

- billing-svc 对外接口未变；新增 `mode=mock/real` 字段于 `/api/v1/pay/channels`；
- 监控指标：`pay_real_mode_active{channel=...}` gauge；
- migrations：0008 新增 `pay_reconcile_records` + `pay_refund_orders`；
- 未来切真：只需注入凭据 → MockMode=false 自动切换 + 全测试通过。

## 复现命令

```bash
cd go/services/billing-svc
go test ./internal/pay/... -run 'Real' -v
go test ./internal/transport/...
```
