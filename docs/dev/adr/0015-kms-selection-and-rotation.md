# ADR-0015：KMS 选型与密钥轮换策略

- 状态：已采纳（第四轮）
- 相关：ADR-0014（身份与密钥统一管理边界）、ADR-0012（MQTT 凭证）
- 影响范围：`pkg/yunmao/kms`、`pkg/yunmao/authjwt`、user-svc、room-svc、device-svc、admin-svc、gateway

## 背景

第三轮已落地 `KeyProvider` 抽象（HS256 / RS256 ephemeral / RS256 PEM），并通过
JWKS endpoint 暴露公钥。但生产环境必须满足：

1. 私钥不能离开受控边界（KMS / HSM / Vault Transit）；
2. key 可周期性轮换，旧 key 在 retire 期内仍可被校验；
3. JWKS endpoint 必须始终返回 active + retiring keys，避免老 token 误被 401；
4. 不同服务（user-svc 登录 token、room-svc 订阅 token、device-svc MQTT 凭证）应能切
   到不同 KMS 域，避免横向爆炸半径。

## 决策

### Provider 选型

| Provider | 用途 | 实现 |
| --- | --- | --- |
| `MockKmsProvider` | 本机 dev / CI / 单测 | 进程内 RSA 2048 + 可选 `VersionStore`（PG 持久化）|
| `VaultKeyProvider` | 内网部署 / 自建 KMS | Hashicorp Vault Transit + KV v2 HTTP API（无 SDK 依赖）|
| `AwsKmsKeyProvider` | 公有云 AWS 部署 | AWS KMS Asymmetric RSA_2048 + `RSASSA_PKCS1_V1_5_SHA_256`（用户注入 `AwsKmsSigner`，避免强依赖 aws-sdk-go-v2）|

> 阿里云 / 华为云 KMS 与 Tencent KMS 同样可以通过实现 `AwsKmsSigner` 接口或
> 单独添加 `AliyunKmsKeyProvider` 来接入；本轮先把抽象与 AWS 占位实现做扎实，
> 第五轮按部署环境选 1–2 家公有云做 SDK 集成。

### 轮换策略（`VersionPolicy`）

- `RotateEvery`：默认 30 天，触发主动 `RotateNow`。
- `RetirePeriod`：默认 7 天，旧 key 在此期间 state=`retiring`，**仍出现在 JWKS**
  但 service 端不再签发新 token；超过后 state=`retired`，从 JWKS 剔除。
- `kid` 命名：`<service>-<unix_nano>`，与服务名前缀一一对应，便于审计与回滚。
- `VersionStore` 接口持久化 `kms_key_versions` 表（migration 0004），让重启不
  丢失 active kid（仅 MockKms 才把私钥也落库；Vault/AWS 永远只持有公钥与元数据）。

### JWKS 暴露规则

- `PublicJWKS()` 始终返回 active + retiring（不返回 retired）。
- gateway / admin / mqtt broker 通过 `JWKSClient`（5 分钟 TTL）拉取，确保
  retire 期内的老 token 仍可校验。
- kid 与 KMS 内部 key id 对齐：
  - MockKms：`mock:{service}-{ts}`；
  - Vault：`vault:{transit_key_name}-{version}`；
  - AWS KMS：`awskms:{alias_or_arn}`。

### 设备 MQTT 凭证

device-svc `IssueMqttCredential` 接收 `KeyProvider` 并按 active key 签 12h 短期
JWT（默认 RS256；HS256 兼容）。EMQX JWT 插件通过 `jwks_url` 校验。轮换时设备
凭证不需要立即重新发放（retire 期内仍可校验），下次心跳 / OTA 拉新即可。

## 不在本轮范围

- AWS KMS / Vault 真实 SDK 调用（占位完成，业务集成留第五轮）。
- HSM / FIPS 140-2 合规栈（按上线区域而定）。
- key 用法分离（jwt-sign vs aead vs tls）：当前所有 KMS provider 只签 RS256 JWT。

## 验证

- `pkg/yunmao/kms` 单测：`MockKmsProvider` 轮换 + persist + Sign/Verify + JWKS 序列化。
- `VaultKeyProvider` 单测：`CachePublicKey` 后 `VerifyingByKid` 返回正确公钥；
  `Active()` 在本轮按设计返回 `not implemented` 错误。
- `AwsKmsKeyProvider` 单测：`Bootstrap` 注入 stub signer 后 `VerifyingByKid` 正常。
- migration 0004：`kms_key_versions` 表上线，索引覆盖 `state`。
