# yunmao Local CI Gate

Automated OpenAPI contract gate for the yunmao shared contract (`go/pkg/yunmao/openapi/v3.json`),
running in the **project-required environment** (this machine).

## Rationale

The yunmao shared contract cannot run in GitHub Actions today because:

1. Parent repo `instructkr/claw-code` denies push access for user `AIseek2025`.
2. Fork repo `AIseek2025/claw-code` accepts data but GitHub rejects workflow files
   — the PAT lacks `workflow` scope, and that scope is required for pushing
   `.github/workflows/*.yml` via `git push` or REST API.
3. GitHub Web UI manual paste requires external human interaction.

As an alternative, this local gate is a **faithful port** of the 4-job GitHub
Actions pipeline (`.github/workflows/openapi-contract.yml`) that runs on the
project's actual environment — the same machine where the contract is developed.

## What runs

| Job                       | Equivalent GitHub Actions Job |
|---------------------------|-------------------------------|
| `spec-lint`               | `spec-lint`                   |
| `gen-typescript-web`      | `gen-typescript-web`          |
| `gen-typescript-admin`    | `gen-typescript-admin`        |
| `contract-consistency`    | `contract-consistency`        |

Each job writes its own log under `reports/local-ci-runs/<RUN_ID>/`.
A `jobs.json` summary reports which passed/failed. Exit 0 means full PASS.

## Manual run

```bash
bash scripts/local-ci/openapi-contract.sh
```

## Scheduled run (cron)

A crontab entry runs the gate at minute 0 of every hour:

```cron
0 * * * * <repo>/scripts/local-ci/openapi-contract.sh >> <repo>/reports/local-ci-runs/cron.log 2>&1
```

Check with `crontab -l`. Append log: `tail -n 60 reports/local-ci-runs/cron.log`.

## Output artifacts

```
reports/local-ci-runs/
├── <YYYYMMDDTHHmmss>/
│   ├── run.log                      # gate overview with timestamps
│   ├── jobs.json                    # structured PASS/FAIL summary
│   ├── spec-lint.log                # Go openapi test output
│   ├── gen-typescript-web.log       # web openapi-gen + tsc + vitest
│   ├── gen-typescript-admin.log     # admin openapi-gen + tsc + vitest
│   └── contract-consistency.log     # SHA256 drift verification
└── cron.log                         # cron invocations summary
```

## Re-enabling GitHub Actions (future)

Once a PAT with `workflow` scope is available:

1. Add the workflow file to the fork:
   ```bash
   git add -f .github/workflows/openapi-contract.yml
   git commit -m "ci: enable openapi-contract workflow"
   git push fork main
   ```
2. The workflow will then run in GitHub Actions. Until then, this local gate
   serves as the project-required-environment CI gate.
