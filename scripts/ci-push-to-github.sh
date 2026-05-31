#!/usr/bin/env bash
# scripts/ci-push-to-github.sh — push yunmao shared-contract artifacts to GitHub
# so that .github/workflows/openapi-contract.yml can run.
#
# HARD LIMITATION (discovered iteration 4):
#   GitHub rejects pushes of workflow files from PATs that lack the `workflow`
#   scope. All attempts to push .github/workflows/* fail with:
#     "refusing to allow a Personal Access Token to create or update workflow
#      .github/workflows/openapi-contract.yml without `workflow` scope"
#
# Resolution paths (pick one):
#   A) Upgrade the PAT to include `workflow` scope, then this script works.
#   B) Manually create the workflow via GitHub web UI:
#        1. Open https://github.com/<owner>/<repo>/actions
#        2. Click "New workflow" → "set up a workflow yourself"
#        3. Paste the contents of isolated_autoruns/yunmao/.github/workflows/openapi-contract.yml
#        4. Commit and let GitHub Actions run
#   C) Use a GitHub App installation token that has workflow permissions.
#
# This script pushes DATA files only (spec + generated types + Go module).
# Workflow file must be added via one of the paths above.
#
# Usage:
#   bash scripts/ci-push-to-github.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PARENT_ROOT="$(cd "$ROOT/.." && pwd)"
BRANCH="feature/yunmao-openapi-contract-$(date +%Y%m%d%H%M%S)"

echo "=== yunmao CI Push (data artifacts only) ==="
echo ""
echo "Repo root:       $ROOT"
echo "Parent root:     $PARENT_ROOT"
echo "Target branch:   $BRANCH"
echo ""
echo "NOTE: Workflow files cannot be pushed via PAT without 'workflow' scope."
echo "      See script header for resolution paths."
echo ""

cd "$PARENT_ROOT"

# Determine which remote to push to (prefer 'fork', fall back to 'origin')
REMOTE=""
if git remote | grep -q '^fork$'; then
  REMOTE="fork"
elif git remote | grep -q '^origin$'; then
  REMOTE="origin"
else
  echo "ERROR: no git remote configured"
  exit 1
fi
echo "Using remote: $REMOTE ($(git remote get-url $REMOTE))"
echo ""

git checkout -b "$BRANCH" main 2>/dev/null || git checkout -b "$BRANCH"

# Force-add the yunmao data tree (overriding .gitignore rule 'isolated_autoruns/')
# NO workflow files — those require workflow scope on the PAT
git add -f \
  "isolated_autoruns/yunmao/go/pkg/yunmao/openapi/v3.json" \
  "isolated_autoruns/yunmao/go/pkg/yunmao/openapi/openapi_test.go" \
  "isolated_autoruns/yunmao/go/pkg/yunmao/go.mod" \
  "isolated_autoruns/yunmao/go/pkg/yunmao/go.sum" \
  "isolated_autoruns/yunmao/clients/web/package.json" \
  "isolated_autoruns/yunmao/clients/web/pnpm-lock.yaml" \
  "isolated_autoruns/yunmao/clients/web/src/lib/generated-api.ts" \
  "isolated_autoruns/yunmao/clients/admin/package.json" \
  "isolated_autoruns/yunmao/clients/admin/pnpm-lock.yaml" \
  "isolated_autoruns/yunmao/clients/admin/src/lib/generated-api.ts" \
  "isolated_autoruns/yunmao/Makefile" \
  "isolated_autoruns/yunmao/scripts/openapi/" 2>/dev/null || true

git commit -m "feat(yunmao): add OpenAPI shared contract + generated TS types for CI

- v3.json: 41 paths, 43 schemas, 9 tags
- web + admin generated-api.ts (openapi-typescript)
- Go module deps + openapi_test.go
- Makefile + gen-types.sh

Workflow file NOT included (requires PAT 'workflow' scope).
Add manually via GitHub web UI."

echo ""
echo "Commit created. Pushing..."
echo ""

git push -u "$REMOTE" "$BRANCH" 2>&1 || {
  echo ""
  echo "ERROR: push failed. Ensure you have push access."
  echo "       If using a fork, run: git remote add fork https://github.com/<you>/claw-code.git"
  exit 1
}

echo ""
echo "=== PUSH SUCCEEDED ==="
echo ""
echo "Branch '$BRANCH' is now on GitHub with all data artifacts."
echo ""
echo "NEXT STEP: Add the workflow file via GitHub web UI."
echo "  1. Go to: https://github.com/<owner>/claw-code/actions"
echo "  2. Click 'New workflow' → 'set up a workflow yourself'"
echo "  3. Paste contents of: isolated_autoruns/yunmao/.github/workflows/openapi-contract.yml"
echo "  4. Commit directly to branch: $BRANCH"
echo ""
echo "After the workflow passes, capture the Actions run ID/log and add to reports/."
