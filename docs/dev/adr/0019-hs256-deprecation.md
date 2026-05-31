# ADR-0019: HS256 兼容路径下线时间表

> 时间：2026-05-25（初版）/ 2026-05-25 第七轮更新  
> 状态：**Implemented**（第七轮：HS256 sign / verify 路径删除）  
> 关联：ADR-0014（identity-and-keys）/ADR-0017（KMS 集成）

## 第七轮变更摘要（2026-05-25）

- `authjwt.NewSigner(secret []byte, issuer string)` / `NewVerifier(secret []byte)` / `NewHSKeyProvider`：
  签名保留，行为改为永远返回 `ErrHS256Removed`（避免外部 callers 编译断裂）。
- `Signer.signClaims` / `Verifier.Parse` 在遇到 `AlgHS256` 或 `alg: HS256` token 时立即返回 `ErrHS256Removed`，
  HS256 加密 token 不会被任何服务接受。
- `EnsureHS256Allowed` 始终返回 `ErrHS256Removed`；`YUNMAO_ALLOW_HS256=true` 不再有效，只会打一次启动 warning。
- 各服务 `cmd/main.go`（user-svc / room-svc / device-svc）当 `YUNMAO_*JWT_ALG=HS256` 时直接 `log.Fatalf` 退出。
- `scripts/perf/ws-baseline-up.sh` 与 `scripts/perf/docker-compose.bench.yml` 切到 `RS256 + JWKS`。
- 测试套件全面切到 `NewRSKeyProviderEphemeral`（`go/pkg/yunmao/authjwt/jwt_test.go`、`device-svc / user-svc / room-svc` 内部 service 测试）。
- 保留：`AlgHS256` 常量与 Prometheus counter `yunmao_authjwt_hs256_usage_total`，用于日志识别与监控历史峰值。

> 后续在下一个 release 周期可选择删除常量与 counter（当 90 天内计数稳定为 0）。
> 本 ADR 的删除 commit 占位：`PR #yunmao-r7-hs256-removal`（待 maintainer 创建）。

## 背景

第三/四/五轮分别落地了 RS256 KeyProvider（本地、Vault Transit、AWS KMS）+
JWKSClient + RemoteSigner，所有服务的签发与校验都已经支持 RS256，
但 HS256 的兼容路径仍保留兜底（NewSigner/NewVerifier、device-svc HS256 SigningMethod、
defaultSecret env）。这种"两条路径并存"的状态在长尾运维里非常危险——
HS256 共享密钥一旦泄漏，所有租户/服务都受影响，而 RS256+JWKS+KMS 的密钥隔离
完全无法弥补。

## 决策

### 第六轮（本轮）：**默认拒绝 HS256**

- 各服务 `cmd/main.go` 在选择 `alg=HS256` 之后立即调用
  `authjwt.EnsureHS256Allowed()`；该函数：
  - 默认返回 `ErrHS256Disabled`（启动 fatal）；
  - 仅当 env `YUNMAO_ALLOW_HS256` ∈ `{true, 1, yes}` 时放行，并：
    - 打 deprecation warning（标准 log，**仅一次**）；
    - Prometheus counter `yunmao_authjwt_hs256_usage_total` +1。
- gateway / verifier 路径：HS256 仍然解析（用于过渡期 verify legacy token），
  但启动日志强烈推荐 `YUNMAO_JWKS_ENDPOINTS` 走 RS256。
- CI（`go.yml`）跑两条 lane：
  - lane A：`YUNMAO_ALLOW_HS256=false`（默认），所有 svc 用 RS256；
  - lane B（兼容回归）：`YUNMAO_ALLOW_HS256=true`，断言 deprecation warning 计数器有增长。

### 第七轮：**完全移除 HS256 兼容路径**

- 删除 `authjwt.NewSigner(secret []byte, ...)` 与 `NewVerifier(secret []byte)` 旧入口；
- 删除 `authjwt.AlgHS256` 常量与 `HSKeyProvider`；
- 各服务 main.go 删除 `case "HS256"` 分支；
- device-svc MQTT 凭证签发器删除 `case authjwt.AlgHS256` 分支；
- ADR-0014 状态 Accepted → Implemented；
- CI 黄/红色 lane B 删除；
- 文档 README / 04 / 06 删除 HS256 配置示例。

### 回滚预案

- 若第六轮上线后 30 天内观察到大量未迁移服务（counter 持续 > 0），重新打开 HS256 默认放行
  + 给业务方 14 天迁移窗口；不要直接删除代码。
- 若第七轮已经合并并触发回滚必要：用 `git revert` 第七轮 commit；HS256 兼容路径可以在
  半天内复活。

## 配置示例

```bash
# 默认（推荐）
YUNMAO_JWT_ALG=RS256
YUNMAO_JWT_RS_PRIVATE_KEY_PATH=/etc/yunmao/keys/user-svc.pem
YUNMAO_JWT_RS_KID=user-svc-2026q2-v1

# 临时兼容旧客户端（限灰度环境）
YUNMAO_JWT_ALG=HS256
YUNMAO_ALLOW_HS256=true
YUNMAO_JWT_HS_SECRET=<≥16 字节 base64>
```

## 影响面

| 模块 | 影响 |
|---|---|
| user-svc / room-svc / device-svc / feeding-svc / billing-svc / admin-svc | 默认 RS256，HS256 需显式开关 |
| gateway WS 鉴权 | 优先 JWKS（RS256），HS256 verify legacy token 仍生效 |
| EMQX JWT 插件 | RS256 + JWKS endpoint（ADR-0017 已对齐） |
| cat-feeder-sim | `--kms=mock` 默认使用 mock KMS；HS256 入口标 deprecated |

## 验证命令

```bash
# 默认路径：HS256 不允许
unset YUNMAO_ALLOW_HS256
YUNMAO_JWT_ALG=HS256 ./bin/user-svc # 应 fatal: HS256 disabled

# 兼容路径：HS256 放行 + warning
YUNMAO_ALLOW_HS256=true YUNMAO_JWT_ALG=HS256 ./bin/user-svc
# 启动日志包含 "[deprecation] YUNMAO_ALLOW_HS256=true ..."

# Prometheus 计数器
curl http://localhost:8101/internal/metrics | grep yunmao_authjwt_hs256_usage_total
```
