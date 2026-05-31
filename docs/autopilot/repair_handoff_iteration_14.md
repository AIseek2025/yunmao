# Repair Handoff Iteration 14

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = waiting_for_fix`

## Read First Paths

1. `reports/work_report_iteration_14.md`
2. `reports/audit_report_iteration_14.md`
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

- `reports/work_report_iteration_15.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。

## Source Audit Report

- `reports/audit_report_iteration_14.md`

## Audit Blocking Summary

- 而 reports/iteration_14_evidence/artifact_inventory.txt 明确列出了远多于 7 条的子证据文件
- work report 声称“85 lines / 81 files / 完整 manifest”，但当前提供的原始 manifest 摘录仍只落盘 7 条，因此报告叙述与可复核落盘物不一致
- 测试仍未在项目要求环境中实际通过目标 CI/GitHub Actions
- 若上述摘要已覆盖当前缺口，不要为了补读长版 `audit_report` 再扩展阅读；只有需要核对原始日志或路径时才回看源报告。

