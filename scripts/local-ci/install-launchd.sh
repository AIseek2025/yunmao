#!/usr/bin/env bash
# scripts/local-ci/install-launchd.sh — install the local-CI gate into macOS launchd.
#
# What this does:
#   1. Copies the OpenAPI contract gate script and LaunchAgent plist to the
#      user's LaunchAgents directory (~/Library/LaunchAgents/).
#   2. Bakes in absolute paths so launchd can resolve them without shell tricks.
#   3. Loads the LaunchAgent via `launchctl bootstrap`, which runs the gate
#      at user login + every 3600 seconds thereafter.
#
# Uninstall:
#   bash scripts/local-ci/install-launchd.sh uninstall
#
# Manual trigger:
#   launchctl kickstart gui/$(id -u)/live.yunmao.local-ci.openapi-contract

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
YUNMAO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LABEL="live.yunmao.local-ci.openapi-contract"
PLIST_DST="$HOME/Library/LaunchAgents/$LABEL.plist"
SCRIPT_SRC="$SCRIPT_DIR/openapi-contract.sh"
SCRIPT_DST="$YUNMAO_ROOT/scripts/local-ci/openapi-contract.sh"

uninstall() {
  echo "=== Uninstalling $LABEL ==="
  if launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null; then
    echo "Job unloaded."
  else
    echo "Job not loaded (nevermind)."
  fi
  [ -f "$PLIST_DST" ] && rm -v "$PLIST_DST"
  exit 0
}

if [ "${1:-}" = "uninstall" ]; then
  uninstall
fi

echo "=== Installing $LABEL ==="
echo "Repo:     $YUNMAO_ROOT"
echo "Gate:     $SCRIPT_SRC"
echo "Plist:    $PLIST_DST"
echo "Interval: 3600s (hourly, + at login)"
echo ""

if [ ! -x "$SCRIPT_SRC" ]; then
  echo "ERROR: Gate script not executable: $SCRIPT_SRC"
  echo "       Run: chmod +x scripts/local-ci/openapi-contract.sh"
  exit 1
fi

mkdir -p "$YUNMAO_ROOT/reports/local-ci-runs"

# Unload existing if present
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true

# Build plist with absolute paths baked in
cat > "$PLIST_DST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string>
    <string>$SCRIPT_DST</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$YUNMAO_ROOT</string>
  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>3600</integer>
  <key>StandardOutPath</key>
  <string>$YUNMAO_ROOT/reports/local-ci-runs/launchd.out.log</string>
  <key>StandardErrorPath</key>
  <string>$YUNMAO_ROOT/reports/local-ci-runs/launchd.err.log</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    <key>HOME</key>
    <string>$HOME</string>
  </dict>
</dict>
</plist>
EOF

echo "Plist written to: $PLIST_DST"

# Bootstrap job into launchd
echo "Bootstrapping into launchd..."
launchctl bootstrap "gui/$(id -u)" "$PLIST_DST"

echo ""
echo "=== LaunchAgent installed ==="
echo ""
echo "The gate will run automatically:"
echo "  - At user login (RunAtLoad=true)"
echo "  - Every 3600 seconds (StartInterval=3600)"
echo ""
echo "Each run writes a timestamped report under:"
echo "  $YUNMAO_ROOT/reports/local-ci-runs/<timestamp>/"
echo ""
echo "Manual trigger:"
echo "  launchctl kickstart gui/$(id -u)/$LABEL"
echo ""
echo "Check status:"
echo "  launchctl print gui/$(id -u)/$LABEL"
echo ""
echo "Uninstall:"
echo "  bash $0 uninstall"
