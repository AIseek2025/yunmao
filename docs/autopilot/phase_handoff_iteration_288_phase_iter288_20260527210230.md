# Phase Handoff Iteration 288

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_287.md`
2. `reports/audit_report_iteration_287.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_287` 已审计通过，可进入 `Phase 2`
- 审计来源：`reports/audit_report_iteration_287.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Target Phase

- `phase_number = 2`
- `phase_title = Admin Productization`

## Target Phase Definition

## Phase B: Admin Productization

目标：

- 收口后台真实可用性，而不是继续停留在占位页面。

建议范围：

1. Admin 登录/鉴权与 `user-svc` 角色体系打通。
2. `/admin/rooms` 与 `/admin/wallet` 接入真实 API。
3. 后台页面与 feature flags、喂食规则、词表治理形成一致的权限与数据流。

退出条件：

- Admin 至少具备真实登录与两个真实业务页面。

## Implementation Entry

- `target_app_root = repo/apps/app/`
- `target_app_root_exists = no`
- `web_reference_root = repo/apps/web/`
- `web_reference_root_exists = no`
- 若 `target_app_root_exists = no`，第一步必须直接在 `repo/apps/app/` 创建工程骨架；只允许把 `repo/apps/web/` 当作参考，不要继续泛读 `repo/` 顶层
- `required_first_write_paths = `repo/apps/app/Cargo.toml`, `repo/apps/app/src/lib.rs`, `repo/apps/app/src/main.rs``
- `allowed_prewrite_reference = repo/Cargo.toml`
- 若无法立即创建上述骨架文件，必须直接写回 `reports/work_report_iteration_288.md` 记录阻塞，而不是继续 Todo/目录探测

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在 Rust 主链推进核心实现，Go 仅做配合层
- 路径基准必须固定为 workspace 根目录：源码位于 `repo/`，规则/报告/交接文档位于 workspace 根目录，禁止把 `rules/`、`reports/`、`docs/autopilot/` 误读成 `repo/...`
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_288.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
