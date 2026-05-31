# yunmao 部署架构

本仓库提供两种部署模式：
- **本地开发**：`docker-compose.dev.yml`（基础设施 + 应用服务）
- **Staging / 生产镜像部署**：`docker-compose.staging.yml` + `.env`

补充说明：
- `make app-up-staging` / `make app-down-staging` / `make logs-staging` / `make deploy-staging` 现在默认使用 `docker-compose.staging.yml --env-file .env`。
- `docker-compose.app.yml` 仍用于本地开发态和 staging-like 对照调试，但不再是默认 staging 发布入口。

⚠️ **不要将 `docker-compose.dev.yml` 直接用于生产环境**，其中包含开发默认密钥。

## 模式对比

| 维度 | 本地开发 | Staging / 生产 |
|------|---------|----------------|
| compose 文件 | `docker-compose.dev.yml` | `docker-compose.staging.yml` |
| 密钥来源 | 环境变量默认值（不安全） | Secrets 注入（Vault / K8s Secret） |
| 支付渠道 | mock 模式 | 真实 sandbox / production |
| 域名 | localhost | staging.yunmao.live |
| TLS | 无 | Let's Encrypt / Cert Manager |

当前仓库内实际执行口径：
- 本地开发应用层：`docker-compose.dev.yml` + `docker-compose.app.yml`
- staging / image-based 应用层：`docker-compose.staging.yml --env-file .env`

## 本地开发

```bash
# 启动基础设施（PostgreSQL / Redis / Kafka / EMQX / MinIO / ClickHouse / Jaeger / Prometheus / Grafana）
make dev-up

# 查看日志
make dev-logs

# 停止
make dev-down
```

### 端口约定

| 服务 | 端口 | 说明 |
|------|------|------|
| PostgreSQL | 5432 | `yunmao:yunmao@localhost:5432/yunmao` |
| Redis | 6379 | 无密码（仅开发） |
| Kafka | 9092 | KRaft 单节点 |
| EMQX (MQTT) | 1883 | 无认证（仅开发） |
| MinIO | 9000 | `minioadmin:minioadmin` |
| ClickHouse | 8123 | 无密码 |
| Jaeger | 16686 | UI |
| Prometheus | 9090 | 监控 |
| Grafana | 3000 | `admin:admin` |

## Staging 部署

### 前置条件

1. 已配置 staging 服务器或 K8s 集群
2. 已配置 secrets 注入机制（K8s Secret / Vault）
3. 已配置域名和 TLS 证书
4. 已配置 TURN 服务凭据
5. 已配置支付 sandbox 凭据

### 快速开始

```bash
# 1. 复制并编辑环境变量
cp .env.example .env.staging

# 2. 构建并推送镜像
make docker-build-all
make docker-push-all

# 3. SSH 到 staging 服务器
ssh ops@staging.yunmao.live

# 4. 克隆仓库并部署
git clone https://github.com/yunmao/yunmao.git
cd yunmao
cp .env.staging .env

# 5. 启动服务
make app-up-staging

# 6. 验证
make deploy-smoke-staging
```

### 自动化部署（推荐）

合并 PR 到 main 分支会自动触发：
1. 运行所有测试（unit / integration）
2. 构建并推送镜像
3. SSH 部署到 staging
4. 运行 smoke test

详见 `.github/workflows/release-staging.yml`。

## 应用服务端口

| 服务 | 端口 | 职责 |
|------|------|------|
| user-svc | 8101 | 用户认证、JWT 签发 |
| room-svc | 8102 | 房间管理、ICE 服务器 |
| device-svc | 8103 | 设备状态管理 |
| billing-svc | 8104 | 支付、订阅 |
| admin-svc | 8105 | 后台 API |
| chat-svc | 8204 | IM 消息 |
| feeding-svc | 8201 | 喂食任务调度 |
| media-edge | 8080 | 媒体转码、HLS |
| gateway | 8090 | WebSocket 网关 |
| device-edge | 8091 | IoT 消息代理 |

## Secrets 注入

### 必需 Secrets

| Secret | 用途 | 注入方式 |
|--------|------|---------|
| `YUNMAO_DB_URL` | PostgreSQL 连接串 | K8s Secret / Vault |
| `YUNMAO_JWT_RS_PRIVATE_KEY_PATH` | JWT 签名私钥 | K8s Secret mounted to `/run/secrets/` |
| `YUNMAO_TURN_SHARED_SECRET` | TURN 临时凭据 | K8s Secret / Vault |
| `YUNMAO_ADMIN_PASSWORD` | 初始管理员密码 | K8s Secret / Vault |

### 可选 Secrets（按需启用）

| Secret | 用途 | 启用条件 |
|--------|------|---------|
| `YUNMAO_WECHAT_*` | 微信支付 | `YUNMAO_ENABLE_WECHAT=true` |
| `YUNMAO_ALIPAY_*` | 支付宝 | `YUNMAO_ENABLE_ALIPAY=true` |
| `YUNMAO_APPLE_*` | Apple IAP | `YUNMAO_ENABLE_APPLEIAP=true` |

详见 [`credential-cutover.md`](../runbooks/credential-cutover.md)。

## 前端部署

### Web 客户端（Next.js）

```bash
# 构建
cd clients/web
cp .env.example .env.local
# 编辑 .env.local 填写真实 API 地址
npm run build

# 启动（生产）
npm run start

# 或使用 Docker
docker build -t yunmao/web:latest -f deploy/Dockerfile.web .
docker run -p 3000:3000 yunmao/web:latest
```

详见 [`clients/web/.env.example`](../../clients/web/.env.example)。

### Admin 客户端（Next.js）

```bash
# 构建
cd clients/admin
cp .env.example .env.local
# 编辑 .env.local 填写真实 API 地址
npm run build

# 启动（生产）
npm run start

# 或使用 Docker
docker build -t yunmao/admin:latest -f deploy/Dockerfile.admin .
docker run -p 3100:3100 yunmao/admin:latest
```

详见 [`clients/admin/.env.example`](../../clients/admin/.env.example)。

## Makefile 目标

### 基础设施（开发）

```bash
make dev-up          # 启动开发基础设施
make dev-down        # 停止并清理
make dev-logs        # 查看日志
```

### 应用服务

```bash
make app-up          # 启动应用服务（依赖 dev 基础设施）
make app-down        # 停止应用服务
make app-restart     # 重启应用服务
```

### Staging 部署

```bash
make deploy-staging  # 构建、推送、部署到 staging
make app-up-staging  # 启动 staging 应用
make app-down-staging # 停止 staging 应用
make deploy-smoke-staging # 运行 smoke test
make logs-staging    # 查看 staging 日志
```

### 数据库迁移

```bash
make migrate-up      # 运行所有迁移（开发）
make migrate-up-staging # 运行所有迁移（staging）
```

### Docker 镜像

```bash
make docker-build-all              # 构建所有 Go + Rust 服务镜像
make docker-push-all               # 推送所有 Go + Rust 服务镜像
make docker-build SVC=user-svc     # 构建单个 Go 服务镜像
make docker-build-rust BIN=yunmao-gateway # 构建单个 Rust 数据面镜像
```

## 监控与观测

### Prometheus

```bash
# 查看指标
open http://localhost:9090

# 常用查询
yunmao_http_requests_total{service="user-svc"}
yunmao_db_query_duration_seconds{service="billing-svc"}
```

### Grafana

```bash
# 访问仪表盘
open http://localhost:3000
# 默认账号：admin / admin
```

### Jaeger（追踪）

```bash
# 查看分布式追踪
open http://localhost:16686
```

## 故障排查

### 服务启动失败

```bash
# 查看日志
make logs-staging SERVICE=user-svc

# 常见原因：
# - 数据库连接失败：检查 YUNMAO_DB_URL
# - JWT 密钥无效：检查 YUNMAO_JWT_RS_PRIVATE_KEY_PATH
# - 端口冲突：检查 8101-8105 / 8080-8091 是否被占用
```

### 数据库迁移失败

```bash
# 查看迁移状态
psql $YUNMAO_DB_URL -c '\dt'

# 手动运行迁移
make migrate-up-staging
```

### 支付回调失败

```bash
# 检查凭据状态
curl http://localhost:8104/internal/diagnose/credentials

# 查看 runbook
cat docs/dev/runbooks/credential-cutover.md
```

## 相关文档

- [Staging 部署 runbook](../runbooks/staging-deploy.md)
- [凭据切换 runbook](../runbooks/credential-cutover.md)
- [TURN 密钥轮换 runbook](../runbooks/turn-credentials-rotation.md)
- [支付 webhook 重放 runbook](../runbooks/billing-webhook-replay.md)
