# Staging Deployment Runbook

> 适用：staging 环境首次部署 / 增量发布 / 回滚

## 前置条件

- [ ] 已有 staging 服务器或 K8s 命名空间
- [ ] 已配置 secrets 注入机制（Vault / AWS Secrets Manager / K8s Secret）
- [ ] 已配置域名解析（`staging.yunmao.live` 或等价测试域名）
- [ ] 已配置 TLS 证书（Let's Encrypt / Cert Manager）
- [ ] 已配置 TURN 服务并获取 shared secret
- [ ] 已配置支付 sandbox 凭据（WeChat / Alipay / Apple IAP）

## 首次部署流程

### 1. 准备环境变量

```bash
# 从 .env.example 复制
cp .env.example .env.staging

# 编辑 .env.staging，填写 staging 环境真实值
# 特别注意：
# - YUNMAO_DB_URL：staging PG 连接串
# - YUNMAO_JWT_RS_PRIVATE_KEY_PATH：指向 staging JWT 私钥
# - YUNMAO_TURN_SHARED_SECRET：staging TURN 密钥
```

### 2. 构建并推送镜像

```bash
# 本地构建
make docker-build-all

# 推送到 registry（以阿里云 ACR 为例）
export REGISTRY=registry.cn-hangzhou.aliyuncs.com/yunmao
make docker-push-all

# 或使用 GitHub Actions（推荐）
# 提交到 main 分支会自动触发 release-staging.yml
```

### 3. 部署应用服务（如使用 docker compose）

```bash
# SSH 到 staging 服务器
ssh ops@staging.yunmao.live

# 克隆仓库
git clone https://github.com/yunmao/yunmao.git
cd yunmao

# 复制环境变量
cp .env.staging .env

# 启动 image-based staging 应用服务
make app-up-staging
```

说明：
- 当前 `make app-up-staging` / `make app-down-staging` / `make logs-staging` 默认使用 `deploy/docker-compose.staging.yml --env-file .env`
- `deploy/docker-compose.app.yml` 保留给本地开发态与 staging-like 对照调试

### 4. 验证部署

```bash
# 检查所有服务健康
make deploy-smoke-staging

# 手动验证
curl https://staging-api.yunmao.live/healthz
curl https://staging-admin.yunmao.live/healthz

# 查看日志
make logs-staging
```

## 增量发布流程

### 触发自动部署

```bash
# 合并 PR 到 main 分支
# GitHub Actions 会自动：
# 1. 运行所有测试（unit / integration）
# 2. 构建镜像
# 3. 推送到 registry
# 4. 通过 SSH 部署到 staging
# 5. 运行 smoke test
```

### 手动发布（紧急修复）

```bash
# 本地构建
make docker-build-all

# 推送
make docker-push-all

# SSH 到 staging 重启服务
ssh ops@staging.yunmao.live
cd yunmao
git pull origin main
make app-restart-staging

# 验证
make deploy-smoke-staging
```

## 回滚流程

### 应用服务回滚

```bash
# SSH 到 staging
ssh ops@staging.yunmao.live
cd yunmao

# 回滚到上一个 Git 版本
git checkout HEAD~1

# 重新构建并重启
make app-restart-staging

# 验证
make deploy-smoke-staging
```

### 数据库回滚

```bash
# 查看最近迁移
ls -ltr go/migrations/

# 手动回滚（示例：回滚 004_create_chat_tables.sql）
psql $YUNMAO_DB_URL -f go/migrations/004_create_chat_tables.down.sql

# 或使用 make 目标（如果定义了 down 迁移）
make migrate-down-to VERSION=003
```

### 完整回滚（包括基础设施）

```bash
# 停止应用
make app-down-staging

# 回滚 Git
git checkout <last-good-commit>

# 重启应用（会自动运行 migrate-up）
make app-up-staging
```

## 监控与告警

### 关键指标

- 服务健康：`make deploy-smoke-staging` 中的 `user-svc / room-svc / device-svc / billing-svc / admin-svc / feeding-svc / media-edge / gateway / device-edge` 均返回 `/healthz=200`
- 数据库连接：支持深探针的服务 `/internal/readyz` 返回 200；旧二进制若未挂载 deep probe，出现 404 需结合 smoke 证据判断
- 支付凭据：`/internal/diagnose/credentials` 返回结构化 JSON，可区分 `missing / partial / ready`
- TURN 服务：`/v1/rooms/{id}/ice-servers` 返回 `ice_servers`，无 shared secret 时允许降级为 STUN-only

### 告警规则（Prometheus）

```bash
# 查看当前告警
curl http://localhost:9090/api/v1/alerts

# 常见问题：
# - yunmao_service_down > 0：某服务宕机
# - yunmao_db_error_total 增长率 > 0：数据库错误
# - yunmao_billing_webhook_bad_sig_total > 0：支付回调验签失败
```

### 日志查看

```bash
# Docker compose（本地开发）
make logs-staging

# K8s（生产）
kubectl -n yunmao-staging logs -f deploy/user-svc
kubectl -n yunmao-staging logs -f deploy/billing-svc
```

## 常见问题

### Q: 镜像拉取失败

```bash
# 检查 registry 登录状态
docker login registry.cn-hangzhou.aliyuncs.com

# 检查镜像是否存在
docker pull registry.cn-hangzhou.aliyuncs.com/yunmao/user-svc:main-abc123
```

### Q: 数据库迁移失败

```bash
# 查看迁移日志
make logs-staging SERVICE=user-svc

# 检查数据库连接
psql $YUNMAO_DB_URL -c '\dt'

# 手动运行迁移
make migrate-up-staging
```

### Q: 支付回调验签失败

```bash
# 检查凭据状态
curl http://localhost:8104/internal/diagnose/credentials

# 查看 runbook
cat docs/dev/runbooks/credential-cutover.md
```

### Q: TURN 连接失败

```bash
# 检查 TURN 凭据
curl http://localhost:8102/v1/rooms/test/ice-servers

# 查看 runbook
cat docs/dev/runbooks/turn-credentials-rotation.md
```

## 相关文档

- [凭据切换 runbook](credential-cutover.md)
- [TURN 密钥轮换 runbook](turn-credentials-rotation.md)
- [支付 webhook 重放 runbook](billing-webhook-replay.md)
- [部署架构](../../deploy/README.md)
