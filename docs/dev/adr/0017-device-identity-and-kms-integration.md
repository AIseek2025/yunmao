# ADR-0017：设备身份与 KMS 集成

- 状态：Accepted（2026-05-25，第五轮）
- 上下文：ADR-0014（身份与密钥统一）、ADR-0015（KMS 选型与轮换）
- 影响范围：`device-svc`、`pkg/yunmao/authjwt`、`pkg/yunmao/kms`、EMQX 部署、`cat-feeder-sim`

## 背景

第四轮已落地 `KeyProvider` 抽象、MockKms 本地轮换、Vault / AWS KMS 占位接口。第五轮要把：

1. 设备 MQTT 凭证从「user-svc 共用 HS256 secret」切换到「device-svc 本地短期 RS256 JWT，公钥发布到 JWKS，EMQX JWT 插件校验」。
2. `KeyProvider.Active()` 在 KMS 后端不再持有私钥，转为「远端签名委托」模式：通过 `authjwt.RemoteSigner` 接口由 `keyprovider.VaultTransit` / `keyprovider.AwsKms` 完成 SHA-256 摘要后调用 KMS Sign。
3. 暴露 `/internal/keys/health` 端点，输出 active / retiring / kid 列表，供 admin 巡检。

## 决策

### A. 设备凭证形态

- Username：`device:<device_id>`；
- Password：device-svc 签发的 RS256 JWT，TTL ≤ 60 分钟；
- Claims：`sub=device_id, scope=device, iss=yunmao.device-svc, aud=yunmao.emqx, iat, exp, jti`；
- kid：与签名 KeyProvider 当前 active kid 对齐。

### B. KMS 后端落地形态

- **MockKmsProvider**：本地 RSA + PG 持久化；CI / dev 默认。
- **VaultTransit (HTTP)**：实现 `keyprovider.NewVaultTransit`，调用 `/v1/transit/sign/<key>/sha2-256`（prehashed=true、pkcs1v15）与 `/v1/transit/keys/<key>`（含 latest_version + 历史版本公钥）；
  - 鉴权：直接 token 或 approle（`/v1/auth/approle/login`）；
  - kid 格式：`transit:<keyname>:v<version>`；JWKS 同时输出 active + 所有历史版本。
- **AwsKms (SDK)**：实现 `keyprovider.NewAwsKms`，由调用方注入 `AwsKmsSigner`（推荐用本仓库附带的 `AwsSdkSigner`，需要 `-tags kms_aws` 编译以避免 aws-sdk-go-v2 强依赖）；
  - 算法：`RSA_2048` + `RSASSA_PKCS1_V15_SHA_256`；
  - kid 格式：`awskms:<KeyId>`。

### C. 签名链

- `KeyProvider.Active()` 返回 `SigningKey{Material: RemoteSigner}`；
- `authjwt.Signer.signClaims` 检测到 Material 实现 `RemoteSigner` 接口时，切换到自定义 `signingMethodRemoteRS256`；
- 该 SigningMethod 把 `signingString` SHA-256 后调用 `RemoteSigner.SignSHA256Digest`，得到的签名直接拼到 token 第 3 段；
- 校验侧统一走标准 RS256 路径（公钥已在 JWKS 缓存）。

### D. 健康检查端点

- `GET /internal/keys/health` 输出：
  ```json
  {
    "service": "user-svc",
    "backend": "vault",
    "active":  "transit:user-svc:v3",
    "kids":    ["transit:user-svc:v2", "transit:user-svc:v3"],
    "vault_url": "http://vault:8200"
  }
  ```
- 服务无 KMS 时退化为基于 JWKS 的 active/kids 列表；
- 暴露在 internal 路径，仅运维网段可达。

### E. EMQX 集成

- 在 `deploy/emqx/jwt-authn.conf`（新增）启用 `jwt` authn，`jwks_endpoint=http://device-svc:8085/jwks.json`，`acl_claim=scope`；
- `cat-feeder-sim` 启动前调 `POST /v1/devices/{id}/mqtt-credential` 取凭证；过期前 5 分钟自动续签（在 `pkg/simdev` 的后续迭代落地，目前轮询凭证）。

## 替代方案

- 让 EMQX 端做密钥落库（不依赖 device-svc）：可行但增加密钥扩散面，放弃。
- 用 OAuth Device Code：流程过重，不适合电子设备「重启即上线」场景。
- 让 user-svc 共签设备凭证：违反职责单一原则，且 user-svc 的 audience 与设备完全不同。

## 影响

- HS256 兼容路径将在第六轮按 ADR-0014 计划下线（保留 `YUNMAO_DEV_HS256` 转开关用于本地脱机）；
- Vault 部署需开启 transit 引擎、为每个服务建一把 RSA 2048 key（`vault write -f transit/keys/user-svc type=rsa-2048`）；
- AWS KMS 需要为每个服务建一把 asymmetric RSA_2048 SIGN_VERIFY key；
- 运维需把 `/internal/keys/health` 列入 Grafana 黑/灰盒巡检面板。
