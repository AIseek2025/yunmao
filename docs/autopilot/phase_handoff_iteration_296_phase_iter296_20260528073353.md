# Phase Handoff Iteration 296

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_295.md`
2. `reports/audit_report_iteration_295.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_295` 已审计通过，可进入 `Phase 6`
- 审计来源：`reports/audit_report_iteration_295.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Target Phase

- `phase_number = 6`
- `phase_title = Staging Parity Smoke And External Integration`

## Target Phase Definition

## Phase 6: Staging Parity Smoke And External Integration

目标：

- 再补齐“部署后真的成立”的证据，把 handler 级/readiness 级证明推进到进程级与环境级验证。

建议范围：

1. 建立 staging 或 staging-like 同构启动路径。
2. 为 healthz / readyz / `/internal/diagnose/credentials` / 关键 API / Web/Admin 访问形成进程级 smoke。
3. 为 TURN、支付、Apple IAP 等外部依赖补齐受控联调 checklist、脚本与证据沉淀入口。
4. 把失败回滚动作纳入 smoke 或 runbook，而不是只停留在文档描述。

退出条件：

- 至少一轮同构 staging smoke 可在仓库内复现。
- 至少一条关键外部依赖闭环形成进程级验证或受控联调证据。
- 所有未完成的外部联调项都被显式标注为 blocker，而不是口头带过。

## Implementation Entry

- `deploy_root = deploy/`
- `scripts_root = scripts/`
- `docs_runbook_root = docs/dev/runbooks/`
- `billing_transport_path = go/services/billing-svc/internal/transport/http.go`
- `room_transport_path = go/services/room-svc/internal/transport/http.go`
- `admin_transport_path = go/services/admin-svc/internal/transport/http.go`
- 当前目标是把仓库内 readiness 证明推进到 staging-like 进程级 smoke：优先补齐启动/诊断脚本、runbook、关键 transport/health 路径与最小受控联调入口。
- `required_first_write_paths = `scripts/credential-check.sh`, `scripts/e2e.sh`, `deploy/docker-compose.app.yml`, `docs/dev/runbooks/credential-cutover.md`, `docs/dev/runbooks/turn-credentials-rotation.md`, `docs/dev/runbooks/staging-smoke.md`, `go/services/billing-svc/internal/transport/http.go`, `go/services/room-svc/internal/transport/http.go`, `go/services/admin-svc/internal/transport/http.go``
- `allowed_reference_paths = deploy, scripts, docs/dev/runbooks, go/services/billing-svc, go/services/room-svc, go/services/admin-svc, clients/web, clients/admin`
- 若无法立即推进 staging smoke 或外部依赖受控联调路径，必须直接写回 `reports/work_report_iteration_296.md` 记录阻塞，不要继续虚构 `repo/apps/app`

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在当前 phase 指定的 `clients/`、`go/`、`rust/`、`deploy/`、`scripts/`、`.github/workflows/` 与 `docs/` 路径推进实现，不允许回退到不存在的 `repo/...` 模板。
- 路径基准必须固定为 workspace 根目录：源码位于 `clients/`、`go/`、`proto/` 等目录，规则/报告/交接文档位于 workspace 根目录，禁止虚构 `repo/...` 前缀。
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_296.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
