# yunmao ECS Production Deployment README

## 1. Production Overview

- Domain: `yunmao.net.cn`
- ECS public IP: `8.218.209.218`
- ECS login user: `admin`
- Production app root: `/opt/yunmao/current`
- Production secrets dir: `/opt/yunmao/shared/secrets`
- ACME webroot: `/var/www/certbot`
- Nginx site config: `/etc/nginx/conf.d/yunmao.net.cn.conf`
- Compose entry: `deploy/docker-compose.ecs.yml`
- Deploy helper: `scripts/yunmao-ecs-deploy.sh`
- Preflight helper: `scripts/yunmao-ecs-preflight-check.sh`

## 2. Isolation Principles

This deployment is placed on a shared ECS that already runs multiple formal projects. `yunmao` must not affect other live services.

Applied isolation rules:

- Independent deployment root: `/opt/yunmao/current`
- Independent secrets root: `/opt/yunmao/shared/secrets`
- Independent Nginx site file: `/etc/nginx/conf.d/yunmao.net.cn.conf`
- Independent TLS certificate: `/etc/letsencrypt/live/yunmao.net.cn/`
- All app ports bind to `127.0.0.1` only
- No reuse of occupied loopback ports from other projects
- No edits to other project compose files, service names, or certificates

## 3. Port Allocation

All service ports are isolated to the `23000/23100/28080+` range:

| Service | Container Port | Host Bind |
|---|---:|---:|
| web | 3000 | `127.0.0.1:23000` |
| admin-web | 3100 | `127.0.0.1:23100` |
| media-edge | 8080 | `127.0.0.1:28080` |
| gateway | 8090 | `127.0.0.1:28090` |
| device-edge | 8091 | `127.0.0.1:28091` |
| user-svc | 8101 | `127.0.0.1:28101` |
| room-svc | 8102 | `127.0.0.1:28102` |
| device-svc | 8103 | `127.0.0.1:28103` |
| billing-svc | 8104 | `127.0.0.1:28104` |
| admin-svc | 8105 | `127.0.0.1:28105` |
| feeding-svc | 8201 | `127.0.0.1:28201` |
| chat-svc | 8204 | `127.0.0.1:28204` |
| feeding gRPC | 9203 | `127.0.0.1:29203` |
| RTMP ingest | 1935 | `127.0.0.1:19350` |

## 4. Runtime Layout

### 4.1 Application Files

- Compose file: `/opt/yunmao/current/deploy/docker-compose.ecs.yml`
- Nginx templates in repo: `deploy/nginx/`
- Production env file: `/opt/yunmao/current/.env.production`

### 4.2 Secrets

Files created under `/opt/yunmao/shared/secrets`:

- `jwt-rs-private.pem`
- `db-password.txt`
- `admin-password.txt`

### 4.3 TLS

- Certificate issued by Certbot for `yunmao.net.cn`
- Active cert path: `/etc/letsencrypt/live/yunmao.net.cn/fullchain.pem`
- Active key path: `/etc/letsencrypt/live/yunmao.net.cn/privkey.pem`

## 5. Key Fixes Applied

This production release required several deploy-specific fixes before the stack became healthy:

1. Kafka runtime
   - Switched image to `apache/kafka:3.8.0`
   - Set Kafka service to run as `root` in ECS compose so the data volume is writable even when Docker creates it as `root:root`

2. Go services secrets access
   - Changed Go runtime container user to `1000:1000`
   - This matches the host `admin` user ownership for bind-mounted secret files

3. Go migrations in containers
   - Embedded the repo `go/migrations` directory into Go runtime images at `/app/migrations`
   - Set runtime env `YUNMAO_MIGRATIONS_DIR=/app/migrations`
   - Fixed `go/pkg/yunmao/db/pg.go` path joining so `os.DirFS(dir)` works correctly with `root="."`

4. macOS metadata pollution
   - Added root `.dockerignore` to exclude `**/._*` and `.DS_Store`
   - Cleaned remote `._*.sql` files that broke migration loading

5. Admin web base path
   - Passed `YUNMAO_ADMIN_BASE_PATH=/admin` plus related public API URLs as Docker build args
   - Rebuilt `admin-web` so Next.js basePath is compiled correctly

6. Nginx admin route
   - Removed the `/admin` <-> `/admin/` redirect loop
   - `location = /admin` now proxies directly to the admin web app

## 6. Nginx Routing

Production entrypoints:

- `https://yunmao.net.cn/` -> web
- `https://yunmao.net.cn/healthz` -> web health
- `https://yunmao.net.cn/admin` -> admin web
- `https://yunmao.net.cn/admin/healthz` -> admin web health
- `https://yunmao.net.cn/admin-api/healthz` -> admin API health
- `https://yunmao.net.cn/ws` -> gateway WebSocket
- `https://yunmao.net.cn/api/v1/auth` and `/users` -> user-svc
- `https://yunmao.net.cn/api/v1/rooms` -> room-svc
- `https://yunmao.net.cn/api/v1/feed-requests` -> feeding-svc
- `https://yunmao.net.cn/api/v1/orders|pay|wallets` -> billing-svc
- `https://yunmao.net.cn/live/` and `/whep/` -> media-edge

## 7. Deployment Procedure

### 7.1 First-Time Provisioning

1. Copy repository to `/opt/yunmao/current`
2. Create `/opt/yunmao/shared/secrets`
3. Generate `.env.production`
4. Install bootstrap Nginx config for ACME if certificate does not exist yet
5. Run Certbot for `yunmao.net.cn`
6. Install formal HTTPS Nginx config and reload Nginx

### 7.2 Normal Deploy

From `/opt/yunmao/current`:

```bash
bash scripts/yunmao-ecs-preflight-check.sh
docker compose -f deploy/docker-compose.ecs.yml --env-file .env.production build
docker compose -f deploy/docker-compose.ecs.yml --env-file .env.production up -d
```

### 7.3 Post-Deploy Checks

Recommended checks:

```bash
curl -ks https://yunmao.net.cn/healthz
curl -ks https://yunmao.net.cn/admin/healthz
curl -ks https://yunmao.net.cn/admin-api/healthz
curl -Iks https://yunmao.net.cn/
curl -Iks https://yunmao.net.cn/admin
docker compose -f deploy/docker-compose.ecs.yml --env-file .env.production ps
```

## 8. Current Verification Result

Validated on `2026-05-31`:

- `https://yunmao.net.cn/` returns `HTTP 200`
- `https://yunmao.net.cn/healthz` returns `{"ok":true,"service":"yunmao-web"}`
- `https://yunmao.net.cn/admin` returns `HTTP 200`
- `https://yunmao.net.cn/admin/healthz` returns `{"ok":true,"service":"yunmao-admin-web"}`
- `https://yunmao.net.cn/admin-api/healthz` returns `ok`
- `docker compose ps` shows all `yunmao` production containers in `Up` state

Core running containers:

- `yunmao-web-prod`
- `yunmao-admin-web-prod`
- `yunmao-user-svc-prod`
- `yunmao-room-svc-prod`
- `yunmao-device-svc-prod`
- `yunmao-billing-svc-prod`
- `yunmao-admin-svc-prod`
- `yunmao-feeding-svc-prod`
- `yunmao-chat-svc-prod`
- `yunmao-gateway-prod`
- `yunmao-device-edge-prod`
- `yunmao-media-edge-prod`
- `yunmao-postgres-prod`
- `yunmao-redis-prod`
- `yunmao-kafka-prod`

## 9. GitHub Mirror

- Target repo: `https://github.com/AIseek2025/yunmao`
- A filtered source-only import was pushed successfully
- GitHub Actions workflow files were intentionally removed during the push because the available Personal Access Token did not include `workflow` scope

If workflow sync is needed later, add a token with `workflow` scope and push `.github/workflows/*` separately.

## 10. Notes

- `scripts/yunmao-ecs-preflight-check.sh` is intended for pre-deploy validation on a stopped or not-yet-bound stack; it will report port conflicts if the production stack is already running
- Do not bind `yunmao` services to public interfaces directly; keep them on `127.0.0.1` behind Nginx
- Do not reuse other project ports or certificates on the shared ECS
