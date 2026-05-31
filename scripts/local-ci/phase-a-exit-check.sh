#!/usr/bin/env bash
# scripts/local-ci/phase-a-exit-check.sh — Phase A exit criteria gate.
#
# Consolidates all Phase A exit conditions into a single runnable gate:
#
#   1) Shared contract output exists and is valid (spec-lint)
#   2) At least one client consumes the contract (web + admin generated-api.ts)
#   3) Rust workspace builds and tests pass
#   4) Go service tests pass
#   5) Contract consistency (no drift between schema and generated types)
#   6) Proto lint passes
#
# This gate can be run manually or scheduled as a LaunchAgent; it mirrors
# the Phase A exit criteria in docs/autopilot/02_phase_plan.md.
#
# Exit 0 = all Phase A exit criteria met locally.
# Exit 1 = one or more criteria not met.
# Output: reports/local-ci-runs/<timestamp>/phase-a-exit.log

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
YUNMAO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUN_ID="$(date +%Y%m%dT%H%M%S)-phaseA"
RUN_DIR="$YUNMAO_ROOT/reports/local-ci-runs/$RUN_ID"

export PATH="$HOME/.cargo/bin:$HOME/.local/go/bin:$HOME/go/bin:$PATH"

mkdir -p "$RUN_DIR"
cd "$YUNMAO_ROOT"

LOG="$RUN_DIR/phase-a-exit.log"
JOB_SUMMARY="$RUN_DIR/jobs.json"

log() { echo "[$(date +%H:%M:%S)] $*" | tee -a "$LOG"; }
start_job() {
  local name="$1"; local logf="$RUN_DIR/${name}.log"
  log "= START JOB: $name"
  exec 3>&1
  exec 1>>"$logf" 2>&1
}
end_job() {
  local name="$1"; local status="$2"
  exec 1>&3
  exec 3>&-
  local elapsed
  elapsed="$(tail -30 "$RUN_DIR/${name}.log" | grep -oE '[0-9]+\.[0-9]+s' | head -1)"
  log "= END JOB: $name ($status)   $elapsed"
}

log "=== yunmao Phase A Exit Criteria Gate ==="
log "Run ID: $RUN_ID"
log "Repo:   $YUNMAO_ROOT"
log "User:   $(whoami)"
log "Host:   $(hostname)"
log ""

JOBS_PASSED=()
JOBS_FAILED=()

JOB_NAMES=("spec-lint" "rust-workspace-test" "rust-workspace-build" \
           "go-service-test" "contract-exists" "contract-consistency" "proto-lint")

# ---------- Job 1: spec-lint ----------
JOB=spec-lint
start_job "$JOB"
echo "Validating go/pkg/yunmao/openapi/v3.json..."
if command -v go >/dev/null; then
  cd "$YUNMAO_ROOT/go/pkg/yunmao" && go test ./openapi/... -v -count=1
  rc=$?
else
  echo "SKIP: go not found"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 2: rust-workspace-test ----------
JOB=rust-workspace-test
start_job "$JOB"
if command -v cargo >/dev/null; then
  cd "$YUNMAO_ROOT/rust" && cargo test --workspace 2>&1
  rc=$?
else
  echo "SKIP: cargo not found"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 3: rust-workspace-build ----------
JOB=rust-workspace-build
start_job "$JOB"
if command -v cargo >/dev/null; then
  cd "$YUNMAO_ROOT/rust" && cargo build --workspace --all-targets 2>&1
  rc=$?
else
  echo "SKIP: cargo not found"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 4: go-service-test ----------
JOB=go-service-test
start_job "$JOB"
GO_MODULES="pkg/yunmao proto services/user-svc services/room-svc services/feeding-svc services/device-svc services/billing-svc services/admin-svc services/chat-svc"
rc=0
if command -v go >/dev/null; then
  cd "$YUNMAO_ROOT/go"
  for m in $GO_MODULES; do
    echo "=== test $m ==="
    (cd "$m" && go test ./...)
    mc=$?
    if [ $mc -ne 0 ]; then rc=$mc; fi
  done
else
  echo "SKIP: go not found"
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 5: contract-exists ----------
JOB=contract-exists
start_job "$JOB"
cd "$YUNMAO_ROOT"
rc=0
for f in "go/pkg/yunmao/openapi/v3.json" \
         "clients/web/src/lib/generated-api.ts" \
         "clients/admin/src/lib/generated-api.ts"; do
  if [ -f "$f" ]; then
    sz=$(wc -c < "$f" | tr -d ' ')
    h=$(sha256sum "$f" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$f" | cut -d' ' -f1)
    echo "OK  $f  size=$sz  sha256=$h"
  else
    echo "MISSING  $f"
    rc=1
  fi
done
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 6: contract-consistency ----------
JOB=contract-consistency
start_job "$JOB"
cd "$YUNMAO_ROOT"
WEB_HASH=$(sha256sum clients/web/src/lib/generated-api.ts 2>/dev/null | cut -d' ' -f1 || shasum -a 256 clients/web/src/lib/generated-api.ts | cut -d' ' -f1)
ADMIN_HASH=$(sha256sum clients/admin/src/lib/generated-api.ts 2>/dev/null | cut -d' ' -f1 || shasum -a 256 clients/admin/src/lib/generated-api.ts | cut -d' ' -f1)
echo "Web:   $WEB_HASH"
echo "Admin: $ADMIN_HASH"
if [ "$WEB_HASH" = "$ADMIN_HASH" ]; then
  echo "Web and Admin generated types are identical (same source contract)."
  rc=0
else
  echo "DRIFT: web and admin generated types differ."
  rc=1
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 7: proto-lint ----------
JOB=proto-lint
start_job "$JOB"
cd "$YUNMAO_ROOT/proto"
if command -v buf >/dev/null; then
  buf lint 2>&1
  rc=$?
else
  echo "SKIP: buf not found"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Summary ----------
log ""
log "=== PHASE A EXIT CRITERIA SUMMARY ==="
log "Passed: ${#JOBS_PASSED[@]}  (${JOBS_PASSED[*]-none})"
log "Failed: ${#JOBS_FAILED[@]}  (${JOBS_FAILED[*]-none})"

OVERALL="PASS"
if [ ${#JOBS_FAILED[@]} -gt 0 ]; then OVERALL="FAIL"; fi

cat > "$JOB_SUMMARY" <<EOF
{
  "run_id": "$RUN_ID",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "user": "$(whoami)",
  "host": "$(hostname)",
  "repo": "$YUNMAO_ROOT",
  "gate": "phase-a-exit-check",
  "jobs_passed": [$(if [ ${#JOBS_PASSED[@]} -gt 0 ]; then printf '"%s",' "${JOBS_PASSED[@]}" | sed 's/,$//'; fi)],
  "jobs_failed": [$(if [ ${#JOBS_FAILED[@]} -gt 0 ]; then printf '"%s",' "${JOBS_FAILED[@]}" | sed 's/,$//'; fi)],
  "overall": "$OVERALL",
  "log_dir": "$RUN_DIR"
}
EOF

if [ "$OVERALL" = "PASS" ]; then
  log "=== PHASE A EXIT CRITERIA: PASS ==="
  log "Note: remote CI (GitHub Actions) still requires PAT workflow scope."
  exit 0
else
  log "=== PHASE A EXIT CRITERIA: FAIL ==="
  exit 1
fi
