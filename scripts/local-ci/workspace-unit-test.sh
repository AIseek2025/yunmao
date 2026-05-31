#!/usr/bin/env bash
# scripts/local-ci/workspace-unit-test.sh — local CI gate for workspace unit tests.
#
# Purpose:
#   Runs Rust workspace tests + Go service-level tests as a local CI gate.
#   Previously these were only manually triggered; this script turns them into
#   an automated, repeatable gate consistent with Phase A CI hardening goals.
#
# Exit 0 = gate PASS (all jobs green).
# Exit 1 = gate FAIL (any job red).
# Output directory: reports/local-ci-runs/<timestamp>/

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
YUNMAO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUN_ID="$(date +%Y%m%dT%H%M%S)-wst"
RUN_DIR="$YUNMAO_ROOT/reports/local-ci-runs/$RUN_ID"

export PATH="$HOME/.cargo/bin:$HOME/.local/go/bin:$HOME/go/bin:$PATH"

mkdir -p "$RUN_DIR"
cd "$YUNMAO_ROOT"

LOG="$RUN_DIR/run.log"
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
  local duration; duration="$(tail -20 "$RUN_DIR/${name}.log" | grep -E 'Duration|elapsed|real' | head -1)"
  log "= END JOB: $name ($status)   $duration"
}

log "=== yunmao local-ci workspace-unit-test ==="
log "Run ID: $RUN_ID"
log "Repo:   $YUNMAO_ROOT"
log "User:   $(whoami)"
log "Host:   $(hostname)"
log ""

JOBS_PASSED=()
JOBS_FAILED=()

# ---------- Job 1: rust-workspace-test ----------
JOB=rust-workspace-test
start_job "$JOB"
cd "$YUNMAO_ROOT"
if command -v cargo >/dev/null; then
  cargo --version
  cd rust
  cargo test --workspace 2>&1
  rc=$?
  cd "$YUNMAO_ROOT"
else
  echo "cargo not installed, skipping"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 2: go-service-test ----------
JOB=go-service-test
start_job "$JOB"
cd "$YUNMAO_ROOT"
if command -v go >/dev/null; then
  go version
  GO_MODULES="pkg/yunmao proto services/user-svc services/room-svc services/feeding-svc services/device-svc services/billing-svc services/admin-svc services/chat-svc"
  rc=0
  cd go
  for m in $GO_MODULES; do
    echo "=== test $m ==="
    (cd "$m" && go test ./...)
    mc=$?
    if [ $mc -ne 0 ]; then rc=$mc; fi
  done
  cd "$YUNMAO_ROOT"
else
  echo "go not installed, skipping"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 3: rust-clippy-lint ----------
JOB=rust-clippy-lint
start_job "$JOB"
cd "$YUNMAO_ROOT"
if command -v cargo >/dev/null; then
  cd rust
  cargo clippy --workspace --all-targets -- -D warnings 2>&1
  rc=$?
  cd "$YUNMAO_ROOT"
else
  echo "cargo not installed, skipping"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 4: go-vet ----------
JOB=go-vet
start_job "$JOB"
cd "$YUNMAO_ROOT"
if command -v go >/dev/null; then
  GO_MODULES="pkg/yunmao proto services/user-svc services/room-svc services/feeding-svc services/device-svc services/billing-svc services/admin-svc services/chat-svc"
  rc=0
  cd go
  for m in $GO_MODULES; do
    echo "=== vet $m ==="
    (cd "$m" && go vet ./...)
    mc=$?
    if [ $mc -ne 0 ]; then rc=$mc; fi
  done
  cd "$YUNMAO_ROOT"
else
  echo "go not installed, skipping"
  rc=0
fi
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Summary ----------
log ""
log "=== SUMMARY ==="
log "Passed: ${#JOBS_PASSED[@]}  (${JOBS_PASSED[*]-none})"
log "Failed: ${#JOBS_FAILED[@]}  (${JOBS_FAILED[*]-none})"

cat > "$JOB_SUMMARY" <<EOF
{
  "run_id": "$RUN_ID",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "user": "$(whoami)",
  "host": "$(hostname)",
  "repo": "$YUNMAO_ROOT",
  "jobs_passed": [$(if [ ${#JOBS_PASSED[@]} -gt 0 ]; then printf '"%s",' "${JOBS_PASSED[@]}" | sed 's/,$//'; fi)],
  "jobs_failed": [$(if [ ${#JOBS_FAILED[@]} -gt 0 ]; then printf '"%s",' "${JOBS_FAILED[@]}" | sed 's/,$//'; fi)],
  "overall": "$([ ${#JOBS_FAILED[@]} -eq 0 ] && echo PASS || echo FAIL)",
  "log_dir": "$RUN_DIR"
}
EOF

if [ ${#JOBS_FAILED[@]} -eq 0 ]; then
  log "=== OVERALL: PASS ==="
  exit 0
else
  log "=== OVERALL: FAIL ==="
  exit 1
fi
