#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/.env.production"
COMPOSE_FILE="${ROOT_DIR}/deploy/docker-compose.ecs.yml"

echo "[preflight] root=${ROOT_DIR}"

for cmd in docker nginx curl openssl; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "[preflight][ERR] missing command: ${cmd}" >&2
    exit 1
  fi
done

if docker compose version >/dev/null 2>&1; then
  echo "[preflight] docker compose available"
else
  echo "[preflight][ERR] docker compose unavailable" >&2
  exit 1
fi

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "[preflight][ERR] missing ${ENV_FILE}" >&2
  exit 1
fi

if [[ ! -f "${COMPOSE_FILE}" ]]; then
  echo "[preflight][ERR] missing ${COMPOSE_FILE}" >&2
  exit 1
fi

SECRETS_DIR="$(grep -E '^YUNMAO_SECRETS_DIR=' "${ENV_FILE}" | head -n1 | cut -d= -f2- || true)"
SECRETS_DIR="${SECRETS_DIR:-/opt/yunmao/shared/secrets}"
KEY_PATH="$(grep -E '^YUNMAO_JWT_RS_PRIVATE_KEY_PATH=' "${ENV_FILE}" | head -n1 | cut -d= -f2- || true)"
KEY_PATH="${KEY_PATH:-/run/secrets/jwt-rs-private.pem}"

HOST_KEY_PATH="${KEY_PATH}"
if [[ "${HOST_KEY_PATH}" == /run/secrets/* ]]; then
  HOST_KEY_PATH="${SECRETS_DIR}/$(basename "${HOST_KEY_PATH}")"
fi

if [[ ! -f "${HOST_KEY_PATH}" ]]; then
  echo "[preflight][WARN] JWT private key not found at ${HOST_KEY_PATH}"
else
  echo "[preflight] jwt key present: ${HOST_KEY_PATH}"
fi

for port in 23000 23100 28080 28090 28091 28101 28102 28103 28104 28105 28201 28204 29203; do
  if ss -ltn "( sport = :${port} )" | grep -q ":${port}"; then
    echo "[preflight][ERR] port already in use: ${port}" >&2
    exit 1
  fi
done

sudo nginx -t >/dev/null
echo "[preflight] nginx config syntax OK"
echo "[preflight] done"
