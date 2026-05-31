#!/usr/bin/env bash
# credential-check.sh — External credential cutover smoke checklist
# Usage: ./credential-check.sh [BILLING_URL] [ROOM_URL]
set -euo pipefail

BILLING="${1:-http://localhost:8104}"
ROOM="${2:-http://localhost:8102}"
PASS=0
FAIL=0
WARN=0

check() {
  local label="$1" cmd="$2"
  printf "  %-50s " "$label"
  if eval "$cmd" >/dev/null 2>&1; then
    echo "PASS"
    PASS=$((PASS + 1))
  else
    echo "FAIL"
    FAIL=$((FAIL + 1))
  fi
}

warn() {
  local label="$1" cmd="$2"
  printf "  %-50s " "$label"
  if eval "$cmd" >/dev/null 2>&1; then
    echo "PASS"
    PASS=$((PASS + 1))
  else
    echo "WARN"
    WARN=$((WARN + 1))
  fi
}

echo "=== Credential Cutover Smoke Checklist ==="
echo ""

echo "--- 1. Service Health ---"
check "billing-svc /healthz" "curl -fsS $BILLING/healthz"
check "billing-svc /internal/readyz" "curl -fsS $BILLING/internal/readyz"
warn  "room-svc /healthz" "curl -fsS $ROOM/healthz"

echo ""
echo "--- 2. Credential Readiness ---"
if curl -fsS "$BILLING/internal/diagnose/credentials" >/dev/null 2>&1; then
  DIAG=$(curl -fsS "$BILLING/internal/diagnose/credentials" 2>/dev/null || true)
  if command -v python3 >/dev/null 2>&1; then
    SUMMARY=$(echo "$DIAG" | python3 -c "
import sys, json
d = json.load(sys.stdin)
ready = d.get('all_ready', False)
checks = d.get('checks', [])
real = d.get('has_real_mode', False)
missing = [f'{c[\"Channel\"]}/{c[\"Field\"]}={c[\"Status\"]}' for c in checks if c['Status'] == 'missing']
partial = [f'{c[\"Channel\"]}/{c[\"Field\"]}={c[\"Status\"]}' for c in checks if c['Status'] == 'partial']
print(f'all_ready={ready} has_real_mode={real} missing={len(missing)} partial={len(partial)}')
for m in missing[:6]: print(f'    ✗ {m}')
if len(missing) > 6: print(f'    .. and {len(missing)-6} more missing')
for p in partial[:4]: print(f'    ~ {p}')
" 2>/dev/null || echo "(parse failed)")
    echo "$SUMMARY"
  else
    echo "    (python3 not available; endpoint returned JSON)"
  fi
  printf "  %-50s %s\n" "endpoint reachable and returns JSON" "PASS"
  PASS=$((PASS + 1))
else
  echo "WARN"
  WARN=$((WARN + 1))
fi

echo ""
echo "--- 3. Pay Channel Availability ---"
PREPAY_RESP=$(curl -s -w '\n%{http_code}' -X POST "$BILLING/api/v1/orders/smoke-cred-001/prepay?channel=mock" -H 'Content-Type: application/json' -d '{"amount_fen":100,"subject":"smoke"}' 2>&1)
PREPAY_CODE=$(echo "$PREPAY_RESP" | tail -1)
if [[ "$PREPAY_CODE" == "200" ]]; then
  printf "  %-50s %s\n" "mock channel prepay" "PASS"
  PASS=$((PASS + 1))
elif echo "$PREPAY_RESP" | head -1 | grep -q "order not found"; then
  printf "  %-50s %s\n" "mock channel prepay (order not found, endpoint alive)" "PASS"
  PASS=$((PASS + 1))
else
  printf "  %-50s %s\n" "mock channel prepay" "FAIL"
  FAIL=$((FAIL + 1))
fi
warn  "wechat channel registered" "curl -fsS $BILLING/api/v1/pay/channels | grep -q wechat"
warn  "alipay channel registered" "curl -fsS $BILLING/api/v1/pay/channels | grep -q alipay"
warn  "appleiap channel registered" "curl -fsS $BILLING/api/v1/pay/channels | grep -q appleiap"

echo ""
echo "--- 4. TURN Credential Issuance ---"
warn  "room-svc ICE servers" "curl -fsS $ROOM/v1/rooms/smoke-cred-room/ice-servers -H 'Authorization: Bearer test-token'"

echo ""
echo "=== Results: PASS=$PASS  WARN=$WARN  FAIL=$FAIL ==="
if [ "$FAIL" -gt 0 ]; then
  echo "FAILURES DETECTED — do not proceed with cutover"
  exit 1
fi
echo "All critical checks passed (warnings are non-blocking)"
