# Repair Handoff Iteration 314

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = waiting_for_fix`

## Read First Paths

1. `.codemaster_orchestration/opencode/dispatch/opencode_session_response_repair_iter315_20260530115124.json`
2. `.codemaster_orchestration/artifacts/opencode_session_stderr_repair_iter315_20260530115124.log`
3. `reports/work_report_iteration_314.md`
4. `reports/audit_report_iteration_314.md`
5. `docs/autopilot/02_phase_plan.md`
6. `docs/autopilot/03_audit_checklist.md`
7. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Auto Diagnosis
- `detected_reason = repair_interrupted_after_meaningful_activity`
- `previous_trace_id = repair_iter315_20260530115124`
- `previous_response_status = interrupted_after_meaningful_activity`
- `previous_returncode = `
- `previous_response_path = .codemaster_orchestration/opencode/dispatch/opencode_session_response_repair_iter315_20260530115124.json`
- `previous_stderr_path = .codemaster_orchestration/artifacts/opencode_session_stderr_repair_iter315_20260530115124.log`
- `diagnosis_summary = 会话已经产生了文本或工具活动，但在形成最终写回前异常中断。`
- `diagnosis_next_action = 优先读取上一次 response/stderr、核对现有代码改动与测试证据；若已形成有效改动则先补写 work_report，否则从中断点继续修复，避免再次从 handoff 顶部重复诊断。`
- `attempt_count = 2`
- `failed_attempt_count = 1`
- `sustained_relaunch_storm = False`
## Recovery Mandate
- 先读取上一次 response 与 stderr 证据，判断为什么会出现空输出、无正文或无报告写回
- 如果上一次实际上已经完成代码与测试，只是漏写 `work_report`，则先补写报告
- 如果上一次没有形成有效交付，则先修复导致中断的原因，再继续当前 phase 修复
- `CodeMaster/GLM-4.5` 作为当前运行中负责人，必须先完成“监测 -> 排查 -> 自动修复 -> 自动重启”：先核对最新 `state.json`、launch observation、response、stderr 与 pending 状态，明确根因类别，再补 completion、清理 stale pending 或补缺失 write-back，最后自动重启当前 repair；禁止等待人工继续。
- 不允许跳过原因排查直接盲目重复同一条空转指令
- 修复时仍要遵守 workspace 路径基准：源码只位于 `repo/` 下；`reports/`、`docs/autopilot/`、`.codemaster_orchestration/` 都在 workspace 根目录，禁止把应用源码路径误写成 `apps/app/*`。
- 当前 App 实现入口固定为 `repo/apps/app/`；若需要查看、glob、grep、edit 或 write App 源码，只允许使用 `repo/apps/app/*`，不要读取或搜索裸 `apps/app/*`。
- 本 handoff 已提取审计阻塞摘要；若这些条目已足够指导下一步，就不要为了补读长版 `audit_report` 再追加新的 `read`。
- 上一轮已经形成具体 Todo 计划且未产生任何代码写回；本轮禁止再次 `todowrite` 或重写同一计划，下一步必须直接进入代码修改、测试，或直接写回 `work_report`。

## Rule

- 当前审计结论为 `needs_followup`
- 当前 phase 不允许进入下一 phase
- 必须继续留在当前 phase 修复 blocking issues 与 required followups
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须重新运行相关测试并写回新的 work_report

## Required Write-Back

- `reports/work_report_iteration_315.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。

## Source Audit Report

- `reports/audit_report_iteration_314.md`

## Audit Blocking Summary

- last_audit_result.decision = "needs_followup"
- 底层异常仍未被隔离：日志继续出现 failed to add snapshot files
- repair 会话依旧只发生 read，未进入 edit / write / bash
- 没有新的进程级 / 环境级 staging smoke 证据
- 若上述摘要已覆盖当前缺口，不要为了补读长版 `audit_report` 再扩展阅读；只有需要核对原始日志或路径时才回看源报告。

