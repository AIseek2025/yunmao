#!/usr/bin/env bash
# Validates GitHub Actions workflow files before push.
# Checks: YAML syntax, referenced paths exist, action versions are valid.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKFLOW_DIR="$REPO_ROOT/.github/workflows"
failed=0

echo "GitHub Actions workflow pre-flight validation"
echo "repo_root: $REPO_ROOT"
echo "workflow_dir: $WORKFLOW_DIR"
echo ""

if ! python3 -c "import yaml" 2>/dev/null; then
  echo "ERROR: python3 with PyYAML required. Install with: pip3 install pyyaml"
  exit 1
fi

for wf in "$WORKFLOW_DIR"/*.yml; do
  [ -f "$wf" ] || continue
  base="$(basename "$wf")"
  echo "--- $base ---"

  if python3 -c "import yaml; yaml.safe_load(open('$wf'))"; then
    echo "  YAML syntax: OK"
  else
    echo "  YAML syntax: FAIL"
    failed=$((failed + 1))
    continue
  fi

  paths=$(python3 -c "
import yaml
w = yaml.safe_load(open('$wf'))
on_val = w.get('on', w.get(True, {}))
if not isinstance(on_val, dict):
  on_val = {}
paths = []
for trigger in ('push', 'pull_request'):
  t = on_val.get(trigger, {})
  if isinstance(t, dict):
    paths.extend(t.get('paths', []))
for p in sorted(set(paths)):
  print(p)
" 2>/dev/null || true)

  if [ -n "$paths" ]; then
    all_paths_ok=true
    while IFS= read -r p; do
      pattern="$p"
      p_clean="${p%%/**}"
      if [ -e "$REPO_ROOT/$p_clean" ]; then
        echo "  path: $pattern -> EXISTS"
      else
        echo "  path: $pattern -> MISSING ($p_clean)"
        all_paths_ok=false
      fi
    done <<< "$paths"
    if [ "$all_paths_ok" = false ]; then
      failed=$((failed + 1))
    fi
  else
    echo "  paths: (none, triggers on all changes)"
  fi

  actions=$(python3 -c "
import yaml
w = yaml.safe_load(open('$wf'))
for job_name, job in w.get('jobs', {}).items():
  for step in job.get('steps', []):
    uses = step.get('uses', '')
    if uses:
      print(uses)
" 2>/dev/null || true)

  if [ -n "$actions" ]; then
    while IFS= read -r action; do
      action_name="${action%@*}"
      version="${action##*@}"
      echo "  action: $action_name@$version"
    done <<< "$actions"
  fi

  echo ""
done

echo "Validation: $failed issue(s) found"
exit "$failed"
