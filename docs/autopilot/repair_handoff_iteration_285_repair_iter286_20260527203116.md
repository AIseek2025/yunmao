# Repair Handoff Iteration 285

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = waiting_for_fix`

## Read First Paths

1. `reports/work_report_iteration_285.md`

## Auto Diagnosis
- `detected_reason = read_only_interrupted_session_without_new_work_report`
- `previous_trace_id = repair_iter286_20260527203027`
- `previous_response_status = interrupted_after_meaningful_activity`
- `previous_returncode = `
- `previous_response_path = .codemaster_orchestration/opencode/dispatch/opencode_session_response_repair_iter286_20260527203027.json`
- `previous_stderr_path = .codemaster_orchestration/artifacts/opencode_session_stderr_repair_iter286_20260527203027.log`
- `diagnosis_summary = 会话已经产生了文本或工具活动，但在形成最终写回前异常中断。`
- `diagnosis_next_action = 旧 response/stderr 只保留为审计元数据，不再作为 repair 必读输入；优先继续修复、测试与写回。`
- `attempt_count = 5`
- `failed_attempt_count = 4`
- `sustained_relaunch_storm = True`
## Recovery Mandate
- 最近一轮属于 repair 只读/空转中断：不要再把旧 `response/stderr` 当作必读输入。
- 本轮先只阅读 `work_report` 与 handoff 内的审计阻塞摘要；禁止直接再读长版 `audit_report`，然后必须直接进入修复、测试或写回。
- 读完最小输入后，下一步动作必须进入 `edit`、`write` 或 `bash`；禁止继续追加 `read/glob/grep`。
- 如果当前只能确认阻塞，也必须先写回新的 `work_report`，不允许空退出或只写 Todo。
- 所有路径都以 workspace 根目录为基准：源码只位于 `repo/` 下；`reports/`、`docs/autopilot/`、`.codemaster_orchestration/` 都在 workspace 根目录，禁止把应用源码路径误写成 `apps/app/*`。
- 当前 App 实现入口固定为 `repo/apps/app/`；若需要查看、glob、grep、edit 或 write App 源码，只允许使用 `repo/apps/app/*`，不要读取或搜索裸 `apps/app/*`。
- 若需要进入 App 代码上下文，禁止读取目录级 `repo/apps/app`；只允许直接读取/编辑具体文件（如 `repo/apps/app/Cargo.toml`、`repo/apps/app/src/*`）或直接执行相关 `bash` 测试/构建命令。
- 当前分支禁止再次使用 `todowrite` 工具或重写 Todo 列表；若已知道缺口，下一步必须直接进入 `edit`、`write`、`bash`，或直接写回 `reports/work_report_iteration_286.md`。
- 本 handoff 已提取审计阻塞摘要；若这些条目已足够指导下一步，就不要为了补读长版 `audit_report` 再追加新的 `read`。
- 当前已进入 relaunch storm：本轮只允许优先读取 `work_report` 与 handoff 内的审计阻塞摘要；不要再把旧 `response/stderr` 或 escalation/incident/recovery/replay 包加入必读列表
- 只有在上述核心证据仍不足以定位根因时，才允许回看 owner packets；否则必须直接进入代码修复、测试或补写 work_report

## Rule

- 当前审计结论为 `needs_followup`
- 当前 phase 不允许进入下一 phase
- 必须继续留在当前 phase 修复 blocking issues 与 required followups
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须重新运行相关测试并写回新的 work_report

## Required Write-Back

- `reports/work_report_iteration_286.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。

## Source Audit Report

- 源 `audit_report` 已被提炼进本 handoff 的审计阻塞摘要；本轮不要直接回读长版源报告，只有在完成代码修改或测试后，确需核对具体日志或路径时才允许回看。

## Audit Blocking Summary

- 远端 GitHub Actions 实跑缺口依旧未关闭；
- 没有新增在项目要求环境中实际跑通的测试证据；
- 本轮新增的 artifact 主要是 repair 会话失败/中断元数据，而非交付能力增强证据。
- 若上述摘要已覆盖当前缺口，不要为了补读长版 `audit_report` 再扩展阅读；只有需要核对原始日志或路径时才回看源报告。

