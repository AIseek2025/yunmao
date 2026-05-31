# Repair Handoff Iteration 317

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = waiting_for_fix`

## Read First Paths

1. `reports/work_report_iteration_317.md`
2. `reports/audit_report_iteration_317.md`
3. `docs/autopilot/02_phase_plan.md`
4. `docs/autopilot/03_audit_checklist.md`
5. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Auto Diagnosis
- `detected_reason = repair_completed_without_new_work_report`
- `previous_trace_id = repair_iter318_20260530124412`
- `previous_response_status = completed`
- `previous_returncode = `
- `previous_response_path = .codemaster_orchestration/opencode/dispatch/opencode_session_response_repair_iter318_20260530124412.json`
- `previous_stderr_path = .codemaster_orchestration/artifacts/opencode_session_stderr_repair_iter318_20260530124412.log`
- `diagnosis_summary = 会话已生成可解析输出。`
- `diagnosis_next_action = 旧 response/stderr 只保留为审计元数据，不再作为 repair 必读输入；优先继续修复、测试与写回。`
- `attempt_count = 2`
- `failed_attempt_count = 0`
- `sustained_relaunch_storm = False`
## Recovery Mandate
- 最近一轮属于 repair 只读/空转中断：不要再把旧 `response/stderr` 当作必读输入。
- 本轮先只阅读 `work_report` 与 handoff 内的审计阻塞摘要；只有摘要不足时才按需回看源 `audit_report`，然后必须直接进入修复、测试或写回。
- 读完最小输入后，下一步动作必须进入 `edit`、`write` 或 `bash`；禁止继续追加 `read/glob/grep`。
- `CodeMaster/GLM-4.5` 作为当前运行中负责人，必须先完成“监测 -> 排查 -> 自动修复 -> 自动重启”：先核对最新 `state.json`、launch observation、response、stderr 与 pending 状态，明确根因类别，再补 completion、清理 stale pending 或补缺失 write-back，最后自动重启当前 repair；禁止等待人工继续。
- 如果当前只能确认阻塞，也必须先写回新的 `work_report`，不允许空退出或只写 Todo。
- 所有路径都以 workspace 根目录为基准：源码只位于 `repo/` 下；`reports/`、`docs/autopilot/`、`.codemaster_orchestration/` 都在 workspace 根目录，禁止把应用源码路径误写成 `apps/app/*`。
- 当前 App 实现入口固定为 `repo/apps/app/`；若需要查看、glob、grep、edit 或 write App 源码，只允许使用 `repo/apps/app/*`，不要读取或搜索裸 `apps/app/*`。
- 本 handoff 已提取审计阻塞摘要；若这些条目已足够指导下一步，就不要为了补读长版 `audit_report` 再追加新的 `read`。

## Rule

- 当前审计结论为 `blocked_on_runtime`
- 当前 phase 不允许进入下一 phase
- 必须继续留在当前 phase 修复 blocking issues 与 required followups
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须重新运行相关测试并写回新的 work_report

## Required Write-Back

- `reports/work_report_iteration_318.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。

## Source Audit Report

- 源 `audit_report` 已被提炼进本 handoff 的审计阻塞摘要；本轮不要直接回读长版源报告，只有在完成代码修改或测试后，确需核对具体日志或路径时才允许回看。

## Audit Blocking Summary

- 只固化运行时证据与阻塞状态
- 根因仍未修复
- 没有出现任何绕过/修复该问题后的写回证据
- 未新增业务代码改动
- 未新增测试执行
- 若上述摘要已覆盖当前缺口，不要为了补读长版 `audit_report` 再扩展阅读；只有需要核对原始日志或路径时才回看源报告。

