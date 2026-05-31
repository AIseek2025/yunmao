#!/usr/bin/env bash
# staging-smoke.sh — Staging parity smoke test for process-level/environment-level validation
# 
# Generates comprehensive evidence for Phase 6 exit conditions:
# - Process-level liveness (/healthz)
# - Environment-level readiness (/internal/readyz)
# - Credential diagnostics (/internal/diagnose/credentials)
# - TURN credential issuance
# - Metrics endpoint availability
# - Key API endpoints (login, room subscription, feeding)
#
# Usage:
#   bash scripts/staging-smoke.sh                           # default localhost
#   YUNMAO_STAGING_HOST=10.0.0.1 bash scripts/staging-smoke.sh
#
# Exit codes:
#   0 = all critical checks passed
#   1 = one or more critical checks failed

set -euo pipefail

HOST="${YUNMAO_STAGING_HOST:-127.0.0.1}"
USER_SVC="${USER_SVC:-http://${HOST}:8101}"
ROOM_SVC="${ROOM_SVC:-http://${HOST}:8102}"
FEEDING_SVC="${FEEDING_SVC:-http://${HOST}:8201}"
BILLING_SVC="${BILLING_SVC:-http://${HOST}:8104}"
ADMIN_SVC="${ADMIN_SVC:-http://${HOST}:8105}"
DEVICE_SVC="${DEVICE_SVC:-http://${HOST}:8103}"
CHAT_SVC="${CHAT_SVC:-http://${HOST}:8204}"
MEDIA_EDGE="${MEDIA_EDGE:-http://${HOST}:8080}"
GATEWAY="${GATEWAY:-http://${HOST}:8090}"
DEVICE_EDGE="${DEVICE_EDGE:-http://${HOST}:8091}"

ROOM_ID="${YUNMAO_STAGING_ROOM:-room_demo}"
PHONE="${YUNMAO_STAGING_PHONE:-+8613${RANDOM}${RANDOM}}"

PASS=0
WARN=0
FAIL=0
EVIDENCE_LOG=""

if [[ -n "${YUNMAO_STAGING_EVIDENCE:-}" ]]; then
  EVIDENCE_LOG="$YUNMAO_STAGING_EVIDENCE"
  echo "Evidence log: $EVIDENCE_LOG"
  echo "=== Staging Smoke Evidence ===" > "$EVIDENCE_LOG"
  echo "Host: $HOST" >> "$EVIDENCE_LOG"
  echo "Timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$EVIDENCE_LOG"
  echo "" >> "$EVIDENCE_LOG"
fi

log_evidence() {
  if [[ -n "$EVIDENCE_LOG" ]]; then
    echo "$@" >> "$EVIDENCE_LOG"
  fi
}

line() {
  printf '\n[staging-smoke] %s\n' "$*"
  log_evidence "$*"
}

check_get() {
  local label="$1" endpoint="$2" expected_code="${3:-200}"
  local actual_code body
  body=$(curl -s -w '\n%{http_code}' "$endpoint" 2>/dev/null || echo -e "\n000")
  actual_code=$(printf '%s' "$body" | tail -1)
  body=$(printf '%s' "$body" | sed '$d')
  
  if [[ "$actual_code" == "$expected_code" ]]; then
    printf '  ✓ %-50s %s\n' "$label" "$actual_code"
    PASS=$((PASS + 1))
    log_evidence "PASS: $label ($actual_code)"
    echo "$body"
    return 0
  else
    printf '  ✗ %-50s %s (expected %s)\n' "$label" "$actual_code" "$expected_code"
    FAIL=$((FAIL + 1))
    log_evidence "FAIL: $label ($actual_code, expected $expected_code)"
    echo "$body"
    return 1
  fi
}

check_post() {
  local label="$1" endpoint="$2" json_body="$3" expected_code="${4:-200}"
  local actual_code body
  body=$(curl -s -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -d "$json_body" "$endpoint" 2>/dev/null || echo -e "\n000")
  actual_code=$(printf '%s' "$body" | tail -1)
  body=$(printf '%s' "$body" | sed '$d')
  
  if [[ "$actual_code" == "$expected_code" ]] || [[ "$actual_code" =~ ^2.. && "$expected_code" =~ ^2.. ]]; then
    printf '  ✓ %-50s %s\n' "$label" "$actual_code"
    PASS=$((PASS + 1))
    log_evidence "PASS: $label ($actual_code)"
    echo "$body"
    return 0
  else
    printf '  ✗ %-50s %s (expected %s)\n' "$label" "$actual_code" "$expected_code"
    FAIL=$((FAIL + 1))
    log_evidence "FAIL: $label ($actual_code, expected $expected_code)"
    echo "$body"
    return 1
  fi
}

check_get_auth() {
  local label="$1" endpoint="$2" token="$3" expected_code="${4:-200}"
  local actual_code body
  body=$(curl -s -w '\n%{http_code}' -H "Authorization: Bearer ${token}" "$endpoint" 2>/dev/null || echo -e "\n000")
  actual_code=$(printf '%s' "$body" | tail -1)
  body=$(printf '%s' "$body" | sed '$d')
  
  if [[ "$actual_code" == "$expected_code" ]]; then
    printf '  ✓ %-50s %s\n' "$label" "$actual_code"
    PASS=$((PASS + 1))
    log_evidence "PASS: $label ($actual_code)"
    echo "$body"
    return 0
  else
    printf '  ✗ %-50s %s (expected %s)\n' "$label" "$actual_code" "$expected_code"
    FAIL=$((FAIL + 1))
    log_evidence "FAIL: $label ($actual_code, expected $expected_code)"
    echo "$body"
    return 1
  fi
}

check_post_auth() {
  local label="$1" endpoint="$2" token="$3" json_body="$4" expected_code="${5:-200}"
  local actual_code body
  body=$(curl -s -w '\n%{http_code}' -X POST -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' -d "$json_body" "$endpoint" 2>/dev/null || echo -e "\n000")
  actual_code=$(echo "$body" | tail -1)
  body=$(echo "$body" | head -n -1)
  
  if [[ "$actual_code" == "$expected_code" ]] || [[ "$actual_code" =~ ^2.. && "$expected_code" =~ ^2.. ]]; then
    printf '  ✓ %-50s %s\n' "$label" "$actual_code"
    PASS=$((PASS + 1))
    log_evidence "PASS: $label ($actual_code)"
    echo "$body"
    return 0
  else
    printf '  ✗ %-50s %s (expected %s)\n' "$label" "$actual_code" "$expected_code"
    FAIL=$((FAIL + 1))
    log_evidence "FAIL: $label ($actual_code, expected $expected_code)"
    echo "$body"
    return 1
  fi
}

check_get_warn() {
  local label="$1" endpoint="$2" expected_code="${3:-200}"
  local actual_code body
  body=$(curl -s -w '\n%{http_code}' "$endpoint" 2>/dev/null || echo -e "\n000")
  actual_code=$(echo "$body" | tail -1)
  body=$(echo "$body" | head -n -1)
  
  if [[ "$actual_code" == "$expected_code" ]]; then
    printf '  ✓ %-50s %s\n' "$label" "$actual_code"
    PASS=$((PASS + 1))
    log_evidence "PASS: $label ($actual_code)"
    echo "$body"
    return 0
  else
    printf '  ~ %-50s %s (expected %s, non-blocking)\n' "$label" "$actual_code" "$expected_code"
    WARN=$((WARN + 1))
    log_evidence "WARN: $label ($actual_code, expected $expected_code)"
    echo "$body"
    return 1
  fi
}

line "Phase 1: Process-level liveness probes (/healthz)"
for svc in user-svc:8101 room-svc:8102 feeding-svc:8201 device-svc:8103 billing-svc:8104 admin-svc:8105 media-edge:8080 gateway:8090 device-edge:8091; do
  name="${svc%%:*}"; port="${svc##*:}"
  check_get "$name /healthz" "http://${HOST}:${port}/healthz" 200 || true
done

line "Phase 2: Environment-level readiness probes (/internal/readyz)"
for svc in user-svc:8101 room-svc:8102 feeding-svc:8201 device-svc:8103 billing-svc:8104 admin-svc:8105; do
  name="${svc%%:*}"; port="${svc##*:}"
  check_get_warn "$name /internal/readyz" "http://${HOST}:${port}/internal/readyz" 200 || true
done

line "Phase 3: Credential diagnostics"
CRED_DIAG=$(check_get "billing-svc /internal/diagnose/credentials" "http://${BILLING_SVC#http://}/internal/diagnose/credentials" 200 || true)
if [[ -n "$CRED_DIAG" ]] && command -v jq >/dev/null 2>&1; then
  echo "  Credential status:"
  echo "$CRED_DIAG" | jq -r 'to_entries[]? | "    \(.key): \(.value)"' 2>/dev/null || echo "    (JSON parse skipped)"
  log_evidence "Credential diagnostics: $CRED_DIAG"
fi

line "Phase 4: Metrics endpoints"
for svc in user-svc:8101 room-svc:8102 feeding-svc:8201 device-svc:8103 billing-svc:8104 admin-svc:8105 gateway:8090 media-edge:8080; do
  name="${svc%%:*}"; port="${svc##*:}"
  check_get_warn "$name /metrics" "http://${HOST}:${port}/metrics" 200 || true
done

line "Phase 5: Key API endpoints — login flow"
LOGIN_BODY=$(check_post "user-svc /v1/auth/login" "${USER_SVC}/v1/auth/login" "{\"phone_e164\":\"${PHONE}\"}" 200 || true)
ACCESS=$(echo "$LOGIN_BODY" | jq -r '.access_token // empty' 2>/dev/null || true)
USER_ID=$(echo "$LOGIN_BODY" | jq -r '.user.id // empty' 2>/dev/null || true)
if [[ -n "$ACCESS" && -n "$USER_ID" ]]; then
  echo "  ✓ Login successful: user_id=$USER_ID"
  log_evidence "Login: user_id=$USER_ID jwt_len=${#ACCESS}"
else
  echo "  ✗ Login failed or incomplete"
  FAIL=$((FAIL + 1))
  log_evidence "Login failed"
fi

line "Phase 6: Key API endpoints — TURN credential issuance"
if [[ -n "$ACCESS" ]]; then
  TURN_CHECK=$(check_get_auth "room-svc /v1/rooms/${ROOM_ID}/ice-servers" "${ROOM_SVC}/v1/rooms/${ROOM_ID}/ice-servers" "$ACCESS" 200 || true)
  if [[ -n "$TURN_CHECK" ]] && command -v jq >/dev/null 2>&1; then
    ICE_COUNT=$(echo "$TURN_CHECK" | jq -r '(.ice_servers // .iceServers // []) | length' 2>/dev/null || echo "0")
    if [[ "$ICE_COUNT" -gt 0 ]]; then
      echo "  ✓ TURN credentials issued: $ICE_COUNT servers"
      PASS=$((PASS + 1))
      log_evidence "TURN: $ICE_COUNT ICE servers"
    else
      echo "  ~ TURN endpoint reachable but no ICE servers configured"
      WARN=$((WARN + 1))
      log_evidence "TURN: endpoint reachable, no ICE servers"
    fi
  fi
else
  echo "  ~ TURN check skipped (no auth token)"
  WARN=$((WARN + 1))
  log_evidence "TURN: skipped (no auth token)"
fi

line "Phase 7: Key API endpoints — feeding request"
if [[ -n "$ACCESS" ]]; then
  IDEM="staging-smoke-$(date +%s)-${RANDOM}"
  FEED_BODY=$(check_post_auth "feeding-svc /api/v1/feed-requests" "${FEEDING_SVC}/api/v1/feed-requests" "$ACCESS" \
    "{\"user_id\":\"${USER_ID}\",\"room_id\":\"${ROOM_ID}\",\"amount_grams\":1,\"idempotency_key\":\"${IDEM}\"}" 201 || true)
  if [[ -n "$FEED_BODY" ]]; then
    FEED_ID=$(echo "$FEED_BODY" | jq -r '.feed_request_id // .id // empty' 2>/dev/null || true)
    if [[ -n "$FEED_ID" ]]; then
      echo "  ✓ Feed request created: $FEED_ID"
      PASS=$((PASS + 1))
      log_evidence "Feed: created feed_request_id=$FEED_ID"
    fi
  fi
else
  echo "  ~ Feeding check skipped (no auth token)"
  WARN=$((WARN + 1))
  log_evidence "Feeding: skipped (no auth token)"
fi

line "Phase 8: Staging deployment validation"
if docker ps --format '{{.Names}}' 2>/dev/null | grep -q 'yunmao-.*-staging'; then
  STAGING_CONTAINERS=$(docker ps --format '{{.Names}}' 2>/dev/null | grep 'yunmao-.*-staging' | wc -l)
  echo "  ✓ Staging containers running: $STAGING_CONTAINERS"
  PASS=$((PASS + 1))
  log_evidence "Staging containers: $STAGING_CONTAINERS"
else
  echo "  ~ No staging containers detected (may be using different naming)"
  WARN=$((WARN + 1))
  log_evidence "Staging containers: not detected"
fi

line "Phase 9: Media edge LL-HLS check (best effort)"
HLS_CHECK=$(curl -s -w '\n%{http_code}' "http://${HOST}:8080/live/${ROOM_ID}/index_ll.m3u8" 2>/dev/null || echo -e "\n000")
HLS_CODE=$(echo "$HLS_CHECK" | tail -1)
if [[ "$HLS_CODE" == "200" ]]; then
  echo "  ✓ LL-HLS playlist available for $ROOM_ID"
  PASS=$((PASS + 1))
  log_evidence "LL-HLS: available for $ROOM_ID"
elif [[ "$HLS_CODE" == "404" ]]; then
  echo "  ~ LL-HLS playlist not yet available (no SPS/PPS yet)"
  WARN=$((WARN + 1))
  log_evidence "LL-HLS: 404 (no stream yet)"
else
  echo "  ~ LL-HLS check returned $HLS_CODE"
  WARN=$((WARN + 1))
  log_evidence "LL-HLS: $HLS_CODE"
fi

line "=== Results: PASS=$PASS  WARN=$WARN  FAIL=$FAIL ==="
log_evidence ""
log_evidence "Results: PASS=$PASS WARN=$WARN FAIL=$FAIL"

if [[ "$FAIL" -gt 0 ]]; then
  echo "CRITICAL: One or more checks failed"
  log_evidence "STATUS: FAILED"
  exit 1
fi

echo "All critical checks passed (warnings are non-blocking)"
log_evidence "STATUS: PASSED"
exit 0
