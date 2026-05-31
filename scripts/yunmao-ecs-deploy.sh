#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/deploy/docker-compose.ecs.yml"
ENV_FILE="${ROOT_DIR}/.env.production"

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "[deploy][ERR] missing ${ENV_FILE}" >&2
  exit 1
fi

bash "${ROOT_DIR}/scripts/yunmao-ecs-preflight-check.sh"

cd "${ROOT_DIR}"

echo "[deploy] building containers..."
docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" build

echo "[deploy] starting stack..."
docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" up -d

echo "[deploy] waiting for local probes..."
for target in \
  "23000/healthz" \
  "28101/healthz" \
  "28102/healthz" \
  "28103/healthz" \
  "28104/healthz" \
  "28105/healthz" \
  "28201/healthz" \
  "28204/healthz" \
  "28080/healthz" \
  "28090/healthz" \
  "28091/healthz"; do
  for _ in $(seq 1 40); do
    if curl -fsS "http://127.0.0.1:${target}" >/dev/null; then
      break
    fi
    sleep 3
  done
  curl -fsS "http://127.0.0.1:${target}" >/dev/null
  echo "[deploy] ok http://127.0.0.1:${target}"
done

echo "[deploy] done"
