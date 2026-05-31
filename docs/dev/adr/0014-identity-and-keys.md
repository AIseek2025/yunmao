# ADR-0014：身份与密钥统一管理边界

- 状态：accepted（第三轮迭代）
- 日期：2026-05-25
- 关联：
  - `go/pkg/yunmao/authjwt/keyprovider.go`
  - `go/pkg/yunmao/authjwt/jwks_client.go`
  - `rust/crates/yunmao-gateway/src/auth.rs`
  - ADR-0007（WebSocket 鉴权）

## 背景

第二轮迭代使用 HS256 + 共享 secret，让 user-svc、room-svc、device-svc 与 gateway
通过 `YUNMAO_JWT_HS_SECRET` 串起来。问题：

1. **每个服务都有完整签发能力**：泄露任何一台都能伪造管理员、设备 token。
2. **密钥轮换困难**：所有副本必须同时重启。
3. **不利于 KMS 接入**：HS secret 无法在 KMS 内"只签不出"。

第三轮目标：把签发与校验分层，为生产引入云 KMS 留出抽象接口。

## 决策

### 算法

- 默认 **RS256**（user-svc、room-svc）：
  - dev：自动生成 ephemeral keypair（启动随机），或从 `YUNMAO_JWT_RS_PRIVATE_KEY_PATH` 加载 PEM；
  - prod：通过 KMS 拿签发句柄，私钥不离开 HSM/KMS。
- 兼容 **HS256**：`YUNMAO_JWT_ALG=HS256` 显式回退，PoC / 离线联调用。
- device-svc 的 MQTT 短期凭证暂时仍走 HS256（device-svc 自签 → broker JWT 鉴权
  plugin 验签），生产切换前打 TODO：使用 KeyProvider.SignDeviceCredential。

### 抽象

```go
type KeyProvider interface {
    Active() (*SigningKey, error)          // 当前签发密钥（包含 kid）
    PublicJWKS() map[string]any            // JWKS 输出（公钥集合）
    VerifyingByKid(kid string) (*VerifyingKey, error) // 按 kid 取校验材料
}
```

实现：

- `hsProvider`：HS256 兼容。
- `rsProvider`：RS256，本地 PEM 或临时生成。
- `JWKSClient`：远端拉 JWKS 缓存，只做校验。
- TODO `kmsProvider`：占位，签发委托给云 KMS（AWS KMS / 阿里云 KMS / Vault Transit）。

### 校验路径

- 各服务自校验：直接持有同一 `KeyProvider`。
- gateway（Rust）：从 user-svc 与 room-svc 的 `/jwks.json` 拉公钥，按 token header
  `kid` 选取对应 `DecodingKey`，5 分钟 TTL 热刷新；找不到 kid 立即触发一次刷新。
- 跨服务校验：服务 B 想接受服务 A 签的 token，配置
  `YUNMAO_JWKS_ENDPOINTS=http://A/jwks.json,http://B/jwks.json`。

### 密钥轮换

- 添加新 kid（双签时段）：`Rotate(newKid, newPriv)` 写入 `rsProvider`，旧 kid 仍可校验。
- 旧 kid 退役：等所有未过期 token 自然过期后从 PublicJWKS 移除。
- gateway 借助 JWKSClient 5min TTL 自动感知新 kid，无需重启。

## 替代方案

1. **EdDSA / Ed25519**：性能更好，曲线安全足够；但 RFC 7517 JWK 兼容性偏弱，
   生态（OAuth2 库 / 移动 SDK）支持不一致。可在二期升级。
2. **HSM 直签 + ASN.1**：完全屏蔽用户态私钥，但开发体验差；后续走 KMS provider 即可。

## 安全注释

- `LoadOrCreateRSKeyProvider` 在 dev 路径下生成 ephemeral key，**重启意味着所有
  已签 token 立即失效**。生产必须显式提供 KMS-backed provider。
- HS256 路径保留 `defaultSecret = "yunmao-dev-shared-secret-please-change-me"`，
  生产部署需要通过 secrets manager 覆盖；启动时检测到默认值时打 warn。
- JWKS endpoint 不需要鉴权，但应放在私网或 SLB 后面。

## 迁移

- 第三轮：user-svc / room-svc 默认 RS256；gateway 可同时支持 HS256 + JWKS；
  device-svc MQTT 凭证 TODO（仍 HS256）。
- 第四轮：device-svc 凭证接 KeyProvider；提供 KMS provider；引入凭证轮换 cron。
- 第五轮：移除 HS256 兼容路径（保留 Verifier 但不再签发）。
