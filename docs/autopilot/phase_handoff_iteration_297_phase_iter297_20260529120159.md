# Phase Handoff Iteration 297

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_296.md`
2. `reports/audit_report_iteration_296.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_296` 已审计通过，可进入 `Phase 7`
- 审计来源：`reports/audit_report_iteration_296.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Target Phase

- `phase_number = 7`
- `phase_title = Closeout Fresh Evidence And Release Readiness`

## Target Phase Definition

## Phase 7: Closeout Fresh Evidence And Release Readiness

目标：

- 不继续泛化扩功能，而是围绕 Phase 6 后仍未闭环的外部输入、TURN、Rust data-plane 与 JWT/JWKS 问题，形成一轮可验证、可审计、可决定去留的 fresh evidence closeout。

建议范围：

1. 先补 Payments / IAP 的真实 round2 after 状态与 diagnostics evidence，明确 WeChat Pay、Alipay、Apple IAP 仍是 `external_wait` 还是已具备真实测试条件。
2. 补 TURN / RTC 的真实 `/v1/rooms/{id}/ice-servers` 结果、credential mode 与 failure mode，不能再只停留在 STUN-only 的旧证据复述。
3. 补 Rust data-plane 与 JWT/JWKS 的 round2 runtime evidence，形成新的 cross-service smoke、runtime matrix 与 alignment 结论。
4. 在三路技术轨道结果齐备后，收敛 owner closeout write-back、decision、blocker closure matrix 与 next action recommendation，并据此判断是否进入 governance refresh。

退出条件：

- Payments / IAP、TURN / RTC、Rust / JWT 三路都已形成 round2 主 write-back 与 leaf evidence，且不再保留模板占位符。
- owner closeout 已能基于 round2 evidence 给出 `continue_closeout | open_new_phase | archive_current_phase` 之一的唯一结论。
- governance refresh 只在 round2 fresh evidence 已改变 blocker 或 owner 结论时才执行；如果只会重复 `waiting_for_fix`，则本 phase 不应以 refresh 完成来冒充收口。

## Implementation Entry

- `workflow_root = .github/workflows/`
- `observability_root = deploy/observability/`
- `perf_root = scripts/perf/`
- `runbook_root = docs/dev/runbooks/`
- 当前目标是补 release gate、rollback 与证据归档：优先在 workflow、观测告警、perf 与 runbook 资产内推进，不要退回产品功能层目录摸排。
- `required_first_write_paths = `.github/workflows/integration.yml`, `.github/workflows/perf.yml`, `.github/workflows/release-staging.yml`, `Makefile`, `scripts/ci-push-to-github.sh`, `scripts/perf/ws-baseline-run.sh`, `deploy/observability/prometheus/alerts/yunmao.rules.yml`, `docs/dev/runbooks/release-gate.md`, `docs/dev/runbooks/rollback-drill.md``
- `allowed_reference_paths = .github/workflows, deploy/observability, scripts/perf, scripts/ci-push-to-github.sh, Makefile, reports/project_assessment_20260528`
- 若无法立即推进发布门禁或回滚强化，必须直接写回 `reports/work_report_iteration_297.md` 记录阻塞，不要继续虚构 `repo/apps/app`

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在当前 phase 指定的 `clients/`、`go/`、`rust/`、`deploy/`、`scripts/`、`.github/workflows/` 与 `docs/` 路径推进实现，不允许回退到不存在的 `repo/...` 模板。
- 路径基准必须固定为 workspace 根目录：源码位于 `clients/`、`go/`、`proto/` 等目录，规则/报告/交接文档位于 workspace 根目录，禁止虚构 `repo/...` 前缀。
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_297.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
