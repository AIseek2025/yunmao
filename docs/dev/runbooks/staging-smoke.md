# Staging Smoke Test Runbook

## Purpose

Run a staging-like smoke against the current docker-compose application stack and collect process-level evidence from live services.

**Scope boundary**:
- This runbook targets the repository's staging-like docker-compose deployment.
- Production should use a stricter procedure with real sandbox/vendor credentials and environment-specific rollback rules.

## Service Matrix

The current smoke scope covers these externally reachable services:

| Service | Port | Required Probe |
|--------|------|----------------|
| `user-svc` | `8101` | `/healthz` |
| `room-svc` | `8102` | `/healthz` |
| `device-svc` | `8103` | `/healthz` |
| `billing-svc` | `8104` | `/healthz`, `/internal/diagnose/credentials` |
| `admin-svc` | `8105` | `/healthz` |
| `feeding-svc` | `8201` | `/healthz` |
| `media-edge` | `8080` | `/healthz` |
| `gateway` | `8090` | `/healthz` |
| `device-edge` | `8091` | `/healthz` |

## Prerequisites

- Staging-like stack started with `make app-up-staging`
- `.env` populated from staging values
- `jq` installed locally
- Network access to ports `8080`, `8090`, `8091`, `8101-8105`, `8201`

## Fast Path

Use the existing scripts as the source of truth:

```bash
# 1. Baseline liveness across all exposed services
make deploy-smoke-staging

# 2. Credential readiness and TURN/ICE baseline
bash scripts/credential-check.sh http://localhost:8104 http://localhost:8102

# 3. End-to-end smoke: login -> room token -> feeding -> playlist -> process probes
bash scripts/e2e.sh
```

## Manual Checks

### 1. Verify Environment File

```bash
test -f .env || { echo "ERROR: .env not found"; exit 1; }
grep -q "YUNMAO_DB_URL" .env || { echo "ERROR: YUNMAO_DB_URL not set"; exit 1; }
grep -q "YUNMAO_JWT_RS_PRIVATE_KEY_PATH" .env || echo "WARN: YUNMAO_JWT_RS_PRIVATE_KEY_PATH not set; services may fall back to ephemeral RS keys"
```

### 2. Verify Running Containers

```bash
docker ps --format "table {{.Names}}\t{{.Status}}" | \
  grep -E "yunmao-(user|room|feeding|device|billing|admin)-svc|yunmao-(media-edge|gateway|device-edge)"
```

### 3. Health Checks

```bash
make deploy-smoke-staging
```

Expected result:
- All 9 endpoints return HTTP 200 on `/healthz`

### 4. Readiness Checks

Only some binaries mount deep readiness probes. Use warnings, not hard failure, for services that still return `404`.

```bash
for svc in user-svc:8101 room-svc:8102 feeding-svc:8201 device-svc:8103 billing-svc:8104 admin-svc:8105; do
  name="${svc%%:*}"
  port="${svc##*:}"
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:${port}/internal/readyz" || true)
  printf '%-12s readyz -> %s\n' "$name" "$code"
done
```

Interpretation:
- `200`: deep readiness is mounted and healthy
- `404`: older binary without deep probe, acceptable if `/healthz` and smoke scripts pass
- `5xx`: investigate before promotion

### 5. Credential Diagnostics

```bash
bash scripts/credential-check.sh http://localhost:8104 http://localhost:8102
```

Interpretation:
- `PASS`: critical path works
- `WARN`: sandbox or vendor credentials are still missing, but mock/staging path remains usable
- `FAIL`: stop and fix before promotion

### 6. TURN / ICE Verification

Current endpoint and field names:
- Endpoint: `GET /v1/rooms/{id}/ice-servers`
- Expected JSON field: `ice_servers`
- Shared secret env: `YUNMAO_TURN_SHARED_SECRET`

```bash
curl -fsS "http://localhost:8102/v1/rooms/room_demo/ice-servers" \
  -H 'Authorization: Bearer test-token' | jq .
```

Interpretation:
- `ice_servers` present with only STUN URLs: endpoint reachable, TURN shared secret not configured yet
- `ice_servers` present with TURN credentials: full TURN path available
- request fails: investigate `room-svc` runtime or auth/setup path

### 7. End-to-End Smoke

```bash
bash scripts/e2e.sh
```

This verifies:
- login and access token issuance
- room subscription token issuance
- feeding request path
- feed status polling
- LL-HLS playlist reachability
- process-level `/healthz`
- best-effort `/internal/readyz`
- credential diagnostics
- TURN/ICE reachability
- `/metrics` exposure

## Failure Actions

### Service Not Healthy

```bash
docker logs <container-name> --tail 100
docker compose -f deploy/docker-compose.app.yml --env-file .env restart <service-name>
```

### Database Connectivity Issues

```bash
grep YUNMAO_DB_URL .env
docker logs yunmao-postgres --tail 50 || true
make migrate-up-staging
```

### Configuration Drift

```bash
make app-down-staging
# edit .env
make app-up-staging
```

## Evidence Collection

Recommended artifacts:

```bash
mkdir -p reports/staging_smoke_evidence

make deploy-smoke-staging | tee reports/staging_smoke_evidence/deploy-smoke.log
bash scripts/credential-check.sh http://localhost:8104 http://localhost:8102 | tee reports/staging_smoke_evidence/credential-check.log
bash scripts/e2e.sh | tee reports/staging_smoke_evidence/e2e.log
curl -s http://localhost:8104/internal/diagnose/credentials > reports/staging_smoke_evidence/credential-diagnostics.json
curl -s -H 'Authorization: Bearer test-token' http://localhost:8102/v1/rooms/room_demo/ice-servers > reports/staging_smoke_evidence/ice-servers.json
```

## Success Criteria

All of the following should hold:

- `make deploy-smoke-staging` passes
- `scripts/credential-check.sh` has no `FAIL`
- `scripts/e2e.sh` completes successfully
- `billing-svc` diagnostics endpoint returns structured JSON
- `room-svc` ICE endpoint returns `ice_servers`
- Gateway and Device-Edge are included in the staging liveness baseline

## Known Limitations

- Missing WeChat / Alipay / Apple IAP sandbox credentials still produce warnings
- Missing `YUNMAO_TURN_SHARED_SECRET` produces STUN-only output instead of full TURN credentials
- Some older binaries may still return `404` on `/internal/readyz`
- This runbook validates a staging-like compose deployment, not a production cluster

## References

- `credential-cutover.md` — payment and credential cutover
- `turn-credentials-rotation.md` — TURN shared secret handling
- `staging-deploy.md` — deployment procedure
- `scripts/credential-check.sh` — credential validation script
- `scripts/e2e.sh` — full smoke workflow
