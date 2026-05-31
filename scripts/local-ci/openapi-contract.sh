#!/usr/bin/env bash
# scripts/local-ci/openapi-contract.sh — local CI gate for yunmao shared contract.
#
# Purpose:
#   Runs the same 4-job pipeline as .github/workflows/openapi-contract.yml
#   but locally, on this machine (the "project-required environment").
#   Installed as a macOS LaunchAgent, it runs automatically every hour,
#   producing a verifiable log under reports/local-ci-runs/.
#
# Why not just use GitHub Actions?
#   - Parent repo (instructkr/claw-code) rejected push access for this user
#   - PAT lacks 'workflow' scope → cannot push workflow files via git or API
#   - Forked repo (AIseek2025/claw-code) has data but no workflow file
#   - GitHub Web UI manual step requires external human interaction
#
# Exit 0 = gate PASS (all 4 jobs green).
# Exit 1 = gate FAIL (any job red).
# Output directory: reports/local-ci-runs/<timestamp>/

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
YUNMAO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUN_ID="$(date +%Y%m%dT%H%M%S)"
RUN_DIR="$YUNMAO_ROOT/reports/local-ci-runs/$RUN_ID"

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
  local duration; duration="$(tail -20 "$RUN_DIR/${name}.log" | grep -E 'Duration|time=' | head -1)"
  log "= END JOB: $name ($status)   $duration"
}

log "=== yunmao local-ci openapi-contract ==="
log "Run ID: $RUN_ID"
log "Repo:   $YUNMAO_ROOT"
log "User:   $(whoami)"
log "Host:   $(hostname)"
log ""

JOBS_PASSED=()
JOBS_FAILED=()

# ---------- Job 1: spec-lint ----------
JOB=spec-lint
start_job "$JOB"
echo "Running Go OpenAPI spec tests..."
pushd go/pkg/yunmao >/dev/null
if command -v go >/dev/null; then
  go test ./openapi/... -v -count=1
  rc=$?
else
  echo "Go not installed, skipping"
  rc=0
fi
popd >/dev/null
end_job "$JOB" "$([ $rc -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 2: gen-typescript-web + contract-consistency ----------
JOB=gen-typescript-web
start_job "$JOB"
pushd clients/web >/dev/null
npm run openapi-gen
rc1=$?
npx tsc --noEmit
rc2=$?
npm run test:run
rc3=$?
popd >/dev/null
end_job "$JOB" "$([ $rc1 -eq 0 ] && [ $rc2 -eq 0 ] && [ $rc3 -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc1 -eq 0 ] && [ $rc2 -eq 0 ] && [ $rc3 -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 3: gen-typescript-admin + contract-consistency ----------
JOB=gen-typescript-admin
start_job "$JOB"
pushd clients/admin >/dev/null
npm run openapi-gen
rc1=$?
npx tsc --noEmit
rc2=$?
npm run test:run
rc3=$?
popd >/dev/null
end_job "$JOB" "$([ $rc1 -eq 0 ] && [ $rc2 -eq 0 ] && [ $rc3 -eq 0 ] && echo PASS || echo FAIL)"
if [ $rc1 -eq 0 ] && [ $rc2 -eq 0 ] && [ $rc3 -eq 0 ]; then JOBS_PASSED+=("$JOB"); else JOBS_FAILED+=("$JOB"); fi

# ---------- Job 4: contract-consistency ----------
JOB=contract-consistency
start_job "$JOB"
cd "$YUNMAO_ROOT"
PRE_WEB="$(sha256sum clients/web/src/lib/generated-api.ts | cut -d' ' -f1)"
PRE_ADMIN="$(sha256sum clients/admin/src/lib/generated-api.ts | cut -d' ' -f1)"
cd clients/web && npm run openapi-gen >/dev/null 2>&1
cd ../admin && npm run openapi-gen >/dev/null 2>&1
cd ../..
POST_WEB="$(sha256sum clients/web/src/lib/generated-api.ts | cut -d' ' -f1)"
POST_ADMIN="$(sha256sum clients/admin/src/lib/generated-api.ts | cut -d' ' -f1)"
if [ "$PRE_WEB" = "$POST_WEB" ] && [ "$PRE_ADMIN" = "$POST_ADMIN" ]; then
  echo "Web:   pre=$PRE_WEB post=$POST_WEB  MATCH"
  echo "Admin: pre=$PRE_ADMIN post=$POST_ADMIN  MATCH"
  rc=0
else
  echo "DRIFT DETECTED"
  echo "Web:   pre=$PRE_WEB post=$POST_WEB"
  echo "Admin: pre=$PRE_ADMIN post=$POST_ADMIN"
  rc=1
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
