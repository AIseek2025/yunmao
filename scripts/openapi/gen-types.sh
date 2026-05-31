#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
SPEC_PATH="$ROOT_DIR/go/pkg/yunmao/openapi/v3.json"

if [ ! -f "$SPEC_PATH" ]; then
  echo "ERROR: OpenAPI spec not found at $SPEC_PATH" >&2
  exit 1
fi

generate_for_client() {
  local client_dir="$ROOT_DIR/clients/$1"
  local out_path="$client_dir/src/lib/generated-api.ts"

  if [ ! -d "$client_dir" ]; then
    echo "SKIP: client $1 not found at $client_dir" >&2
    return
  fi

  mkdir -p "$(dirname "$out_path")"

  echo "Generating types for clients/$1 → $out_path"
  npx openapi-typescript "$SPEC_PATH" -o "$out_path" --export-type
}

if [ $# -eq 0 ]; then
  generate_for_client "web"
  generate_for_client "admin"
else
  for c in "$@"; do
    generate_for_client "$c"
  done
fi
