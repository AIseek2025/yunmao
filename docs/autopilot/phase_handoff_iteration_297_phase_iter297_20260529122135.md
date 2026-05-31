# Phase Handoff Iteration 297

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `.codemaster_orchestration/opencode/dispatch/opencode_session_response_phase_iter297_20260529122039.json`
2. `.codemaster_orchestration/artifacts/opencode_session_stderr_phase_iter297_20260529122039.log`
3. `reports/work_report_iteration_296.md`
4. `reports/audit_report_iteration_296.md`
5. `docs/autopilot/02_phase_plan.md`
6. `docs/autopilot/03_audit_checklist.md`
7. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_296` 已审计通过，可进入 `Phase 7`
- 审计来源：`reports/audit_report_iteration_296.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Auto Diagnosis
- `detected_reason = phase_interrupted_after_meaningful_activity_without_write_report_only`
- `previous_trace_id = phase_iter297_20260529122039`
- `previous_response_status = interrupted_after_meaningful_activity`
- `previous_returncode = `
- `previous_response_path = .codemaster_orchestration/opencode/dispatch/opencode_session_response_phase_iter297_20260529122039.json`
- `previous_stderr_path = .codemaster_orchestration/artifacts/opencode_session_stderr_phase_iter297_20260529122039.log`
- `diagnosis_summary = 会话已经产生了文本或工具活动，但在形成最终写回前异常中断。`
- `diagnosis_next_action = 优先读取上一次 response/stderr、核对现有代码改动与测试证据；若已形成有效改动则先补写 work_report，否则从中断点继续修复，避免再次从 handoff 顶部重复诊断。`
- `attempt_count = 8`
- `failed_attempt_count = 7`
- `sustained_relaunch_storm = True`
## Recovery Mandate
- 先核对上一次 phase response/stderr 与现有代码改动，判断为什么反复失败却没有写回 work_report
- 如果上一次实际上已经完成代码与测试，只是漏写 `work_report`，则先补齐报告，再继续当前 phase 收尾
- 所有路径都以 workspace 根目录为基准：源码在 `clients/`、`go/`、`proto/` 等目录，`reports/`、`docs/autopilot/` 都在 workspace 根目录下，禁止误写成 `repo/...`。
- 当前实现入口固定为 `reports/closeout/20260529-*-round2/`、round2 tracker 与 `docs/execution/20260529-yunmao-phase6-planning/`；优先补齐 round2 fresh evidence、三路 track write-back 与 owner closeout，不要回退到旧的 release gate / workflow 资产。
- 若继续推进 Phase 7，新增写入必须优先落在 round2 tracker、三路 round2 write-back 或 owner closeout write-back；不要回到旧的 workflow、release gate 或 rollback 模板继续泛读。
- 允许按需引用 `reports/closeout/`、`reports/iteration_296_evidence/`、`reports/codemaster/`、`docs/execution/20260529-yunmao-phase6-planning/`、`docs/dev/runbooks/` 与 `deploy/docker-compose.staging.yml`；读完后必须直接进入 round2 证据回填或写回 `reports/work_report_iteration_297.md`。
- 在重新阅读完关键证据后，下一步动作必须转入实现、测试或写回，不允许再次回到全量诊断循环
- 如果上一次没有形成有效交付，则必须先修复导致 phase 中断的根因，再继续当前 phase
- 若当前仍无法完成交付，也必须先写回 `work_report` 记录阻塞点与已验证证据
- 若无法继续推进实现，禁止只写 Todo 后退出；必须直接写回 `reports/work_report_iteration_297.md` 记录当前阻塞、缺失前提与已验证证据
- 若已出现累计 relaunch storm，不允许继续按同一空转路径重复尝试；必须显式调整排障策略并给出可审计的写回结果
- 当前已进入 relaunch storm：本轮只允许优先读取 `response/stderr + work_report + audit_report + 当前 phase 规则`，不要再次从 escalation/incident/recovery/replay 包顶部重读
- 只有在上述核心证据仍不足以定位根因时，才允许回看 owner packets；否则必须直接进入代码修复、测试或补写 work_report

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

- `closeout_root = reports/closeout/`
- `planning_packet_root = docs/execution/20260529-yunmao-phase6-planning/`
- `payments_round2_root = reports/closeout/20260529-payments-iap-round2/`
- `turn_round2_root = reports/closeout/20260529-turn-rtc-round2/`
- `rust_round2_root = reports/closeout/20260529-rust-jwt-round2/`
- `owner_round2_root = reports/closeout/20260529-owner-closeout-round2/`
- 当前目标是补 round2 fresh evidence 与 owner closeout：优先在 `reports/closeout/20260529-*-round2/`、round2 tracker 与 owner brief 上推进，不要回退到旧的 release gate / workflow 资产。
- `required_first_write_paths = `reports/closeout/20260529-ROUND2-OWNER-OPERATING-BRIEF.md`, `reports/closeout/20260529-ROUND2-PROGRESS-TRACKER.md`, `reports/closeout/20260529-payments-iap-round2/summary.md`, `reports/closeout/20260529-payments-iap-round2/payments_iap_writeback.md`, `reports/closeout/20260529-turn-rtc-round2/summary.md`, `reports/closeout/20260529-turn-rtc-round2/turn_rtc_writeback.md`, `reports/closeout/20260529-rust-jwt-round2/summary.md`, `reports/closeout/20260529-rust-jwt-round2/rust_jwt_writeback.md`, `reports/closeout/20260529-owner-closeout-round2/owner_closeout_writeback.md``
- `allowed_reference_paths = reports/closeout, reports/iteration_296_evidence, reports/codemaster, docs/execution/20260529-yunmao-phase6-planning, docs/dev/runbooks, deploy/docker-compose.staging.yml`
- 若无法立即推进 round2 fresh evidence，也必须直接写回 `reports/work_report_iteration_297.md` 记录缺失输入、证据缺口与下一步，不要继续虚构 `repo/apps/app`

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
