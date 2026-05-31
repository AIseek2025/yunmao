# Repair Handoff Iteration 19

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = waiting_for_fix`

## Read First Paths

1. `reports/work_report_iteration_19.md`
2. `reports/audit_report_iteration_19.md`
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

- `reports/work_report_iteration_20.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。

## Source Audit Report

- `reports/audit_report_iteration_19.md`

## Audit Blocking Summary

- 当前可见通过证据仍为 brandos-MacBook-Pro-2.local 上的 local-ci，并非远程/正式 CI 门禁
- 且 reports/iteration_19_evidence/artifact_inventory.txt 中 audit_payload_iteration_19.json 的 size/sha256 与顶层 manifest、证据目录摘录存在直接冲突
- 此外，本轮最主要的修复目标是报告与 artifact 一致性，但原始 artifact 仍存在明显矛盾，说明审计物料本身仍不可靠
- iteration 18 的“报告内自引用 hash / manifest 一致性”问题仅部分修复，未完全收口
- Phase A 共享契约链路仍然成立
- 若上述摘要已覆盖当前缺口，不要为了补读长版 `audit_report` 再扩展阅读；只有需要核对原始日志或路径时才回看源报告。

