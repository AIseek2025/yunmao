#!/usr/bin/env bash
# Phase A verification script — reproduces the full evidence set across 12 gates.
# Usage: scripts/phase-a-verify.sh [output_dir]
# Exit: 0 if all gates pass, non-zero otherwise.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${1:-$REPO_ROOT/reports/iteration_evidence_$(date +%Y%m%d_%H%M%S)}"
mkdir -p "$OUT_DIR"

failed=0
run() {
  local name="$1"
  local log="$OUT_DIR/${name}.txt"
  shift
  echo "[$(date '+%H:%M:%S')] $name"
  if "$@" > "$log" 2>&1; then
    echo "  PASS -> $log"
  else
    echo "  FAIL -> $log (exit=$?)"
    failed=$((failed + 1))
  fi
}

run_shell() {
  local name="$1"
  local cmd="$2"
  local log="$OUT_DIR/${name}.txt"
  echo "[$(date '+%H:%M:%S')] $name"
  if bash -c "$cmd" > "$log" 2>&1; then
    echo "  PASS -> $log"
  else
    echo "  FAIL -> $log (exit=$?)"
    failed=$((failed + 1))
  fi
}

run "cargo_test"    bash -c "cd '$REPO_ROOT/rust' && cargo test --workspace --all-targets"
run_shell "cargo_fmt"     "cd '$REPO_ROOT/rust' && cargo fmt --all -- --check; echo EXIT=\$?"
run_shell "cargo_clippy"  "cd '$REPO_ROOT/rust' && cargo clippy --workspace --all-targets -- -D warnings; echo EXIT=\$?"
run "go_test_openapi" bash -c "cd '$REPO_ROOT/go' && go test ./pkg/yunmao/openapi/... -v -count=1"
run_shell "go_build_openapi" "cd '$REPO_ROOT/go' && go build ./pkg/yunmao/openapi/...; echo EXIT=\$?"
run_shell "go_vet"        "cd '$REPO_ROOT/go' && go vet ./pkg/yunmao/openapi/...; echo EXIT=\$?"
run_shell "go_mod_verify" "cd '$REPO_ROOT/go/pkg/yunmao' && go mod verify; echo EXIT=\$?"
run "ci_workflows"    bash -c "cd '$REPO_ROOT' && bash scripts/validate-ci-workflows.sh"
run "web_vitest"     bash -c "cd '$REPO_ROOT/clients/web' && npx vitest run"
run_shell "web_tsc"       "cd '$REPO_ROOT/clients/web' && npx tsc --noEmit; echo EXIT=\$?"
run "admin_vitest"   bash -c "cd '$REPO_ROOT/clients/admin' && npx vitest run"
run_shell "admin_tsc"     "cd '$REPO_ROOT/clients/admin' && npx tsc --noEmit; echo EXIT=\$?"

rm -f "$OUT_DIR/00_manifest.txt"
{
  echo "Phase A verification manifest"
  echo "generated: $(date -u '+%Y-%m-%d %H:%M:%SZ')"
  echo "repo_root: $REPO_ROOT"
  echo "out_dir:   $OUT_DIR"
  echo "failures:  $failed"
  echo ""
  printf "%-25s %8s  %s\n" artifact size sha256
  for f in "$OUT_DIR"/*.txt; do
    [ -f "$f" ] || continue
    base="$(basename "$f")"
    [ "$base" = "00_manifest.txt" ] && continue
    sz="$(wc -c < "$f" | tr -d ' ')"
    sha="$(shasum -a 256 "$f" | cut -c1-16)"
    printf "%-25s %8s  %s\n" "$base" "$sz" "$sha"
  done
} > "$OUT_DIR/00_manifest.txt"
cat "$OUT_DIR/00_manifest.txt"

if [ "$failed" -eq 0 ]; then
  echo ""
  echo "Phase A verification: ALL GATES PASSED (evidence in $OUT_DIR)"
  exit 0
else
  echo ""
  echo "Phase A verification: $failed GATE(S) FAILED (see $OUT_DIR)"
  exit 1
fi
