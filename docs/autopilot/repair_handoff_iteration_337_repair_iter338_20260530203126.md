# Repair Handoff Iteration 337

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = waiting_for_fix`

## Read First Paths

1. `reports/work_report_iteration_337.md`
2. `reports/audit_report_iteration_337.md`
3. `docs/autopilot/02_phase_plan.md`
4. `docs/autopilot/03_audit_checklist.md`
5. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Rule

- 当前审计结论为 `needs_followup`
- 当前 phase 不允许进入下一 phase
- 必须继续留在当前 phase 修复 blocking issues 与 required followups
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须重新运行相关测试并写回新的 work_report

## Required Write-Back

- `reports/work_report_iteration_338.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。

## Source Audit Report

- `reports/audit_report_iteration_337.md`

## Audit Blocking Summary

- 本次提供的 supporting artifacts 中并未附上该规划文档原文摘录，也没有目录变更、diff、文件内容、提交片段或其他原始证据来证明该文档确已落盘且内容与叙述一致；
- 已提供的证据几乎全部是 iteration 336 已经审过的测试主线的再次执行或缓存命中，并不能直接证明 iteration 337 的新增交付物已经形成。
- 复跑/复用了一组已有基线测试与 round2 证据测试，且 exit code 为 0；
- 若上述摘要已覆盖当前缺口，不要为了补读长版 `audit_report` 再扩展阅读；只有需要核对原始日志或路径时才回看源报告。

