#!/usr/bin/env bash
# 校验 + 生成 yunmao proto。
#
# - 校验 JSON / YAML schema（CI 兜底）
# - 使用 buf lint / build / generate 生成 go gRPC stub
# - 不强制 buf breaking（CI 单独做，比较 origin/main）
#
# 依赖：buf、protoc-gen-go、protoc-gen-go-grpc（go install）。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
proto_dir="${ROOT}/proto"

export PATH="${HOME}/.cargo/bin:${HOME}/go/bin:${HOME}/.local/go/bin:${PATH}"

echo "[gen-proto] checking JSON schemas under ${proto_dir}/cloudevents/"
for f in "${proto_dir}"/cloudevents/*.json; do
  python3 -c "import json; json.load(open('${f}'))" >/dev/null
  echo "  ok: $(basename "${f}")"
done

echo "[gen-proto] checking yaml under ${proto_dir}/errors/"
python3 -c "import yaml; yaml.safe_load(open('${proto_dir}/errors/codes.yaml'))" >/dev/null

if ! command -v buf >/dev/null 2>&1; then
  echo "[gen-proto] buf not installed; install via 'go install github.com/bufbuild/buf/cmd/buf@latest'"
  exit 0
fi

echo "[gen-proto] buf lint"
( cd "${proto_dir}" && buf lint )

echo "[gen-proto] buf build (验证 proto 编译完整)"
( cd "${proto_dir}" && buf build )

echo "[gen-proto] buf generate"
mkdir -p "${ROOT}/go/proto"
( cd "${proto_dir}" && buf generate )

echo "[gen-proto] done; generated under go/proto/"
find "${ROOT}/go/proto" -name '*.go' -print | head -20
