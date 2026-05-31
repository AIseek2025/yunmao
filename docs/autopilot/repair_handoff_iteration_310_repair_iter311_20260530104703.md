# Repair Handoff Iteration 310

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = waiting_for_fix`

## Read First Paths

1. `reports/work_report_iteration_310.md`
2. `.codemaster_orchestration/artifacts/opencode_session_stderr_repair_iter311_20260530104145.log`
3. `reports/audit_report_iteration_310.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Auto Diagnosis
- `detected_reason = launch_stale_without_response`
- `previous_trace_id = repair_iter311_20260530104145`
- `previous_response_status = `
- `previous_returncode = `
- `previous_response_path = missing`
- `previous_stderr_path = .codemaster_orchestration/artifacts/opencode_session_stderr_repair_iter311_20260530104145.log`
- `attempt_count = 2`
- `failed_attempt_count = 0`
- `sustained_relaunch_storm = False`
## Recovery Mandate
- 先读取上一次 response 与 stderr 证据，判断为什么会出现空输出、无正文或无报告写回
- 如果上一次实际上已经完成代码与测试，只是漏写 `work_report`，则先补写报告
- 如果上一次没有形成有效交付，则先修复导致中断的原因，再继续当前 phase 修复
- 不允许跳过原因排查直接盲目重复同一条空转指令
- 修复时仍要遵守 workspace 路径基准：源码只位于 `repo/` 下；`reports/`、`docs/autopilot/`、`.codemaster_orchestration/` 都在 workspace 根目录，禁止把应用源码路径误写成 `apps/app/*`。
- 当前 App 实现入口固定为 `repo/apps/app/`；若需要查看、glob、grep、edit 或 write App 源码，只允许使用 `repo/apps/app/*`，不要读取或搜索裸 `apps/app/*`。
- 本 handoff 已提取审计阻塞摘要；若这些条目已足够指导下一步，就不要为了补读长版 `audit_report` 再追加新的 `read`。

## Rule

- 当前审计结论为 `needs_followup`
- 当前 phase 不允许进入下一 phase
- 必须继续留在当前 phase 修复 blocking issues 与 required followups
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须重新运行相关测试并写回新的 work_report

## Required Write-Back

- `reports/work_report_iteration_311.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。

## Source Audit Report

- `reports/audit_report_iteration_310.md`

## Audit Blocking Summary

- 仍停留在 process-level fresh evidence 阶段
- 未推进到 environment-level staging parity closeout
- 仅为证据物化与汇总
- 未新增业务代码
- 缺少进
- 若上述摘要已覆盖当前缺口，不要为了补读长版 `audit_report` 再扩展阅读；只有需要核对原始日志或路径时才回看源报告。

