# Phase Handoff Iteration 299

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_298.md`
2. `reports/audit_report_iteration_298.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_298` 已审计通过，可进入 `Phase 8`
- 审计来源：`reports/audit_report_iteration_298.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转


负责人接管协议：
- `owner_action = continue_development`
- `takeover_mode = continue_development`
- `owner_judgment = 负责人判断当前可继续编码推进。 当前主目标: 1. 本轮作为 iteration 297 审计放行的 repair iteration，目标是补齐真实 write-back 并纠正 round2 closeout 中的事实性错误。`
- `restart_strategy = restart_when_runtime_interrupted`
- 必做下一步: 先按负责人判断执行 `continue_development`，推荐入口 `development`。
- 必做下一步: 先读 `project_owner_read_pack` 与 handoff，再进入写入、验证或恢复动作。
- 必做下一步: 优先把当前 phase 的最小实现、验证或报告闭环补齐。
- 禁止动作: 禁止跳过负责人必读包后直接盲目编码或重复全量读仓库。
- 禁止动作: 禁止在同一问题已进入 relaunch storm / repeated repair 后继续沿用旧 prompt 空转。
- 禁止动作: 禁止在需要最小修补时擅自扩大成跨模块、跨 phase 的泛化重构。
- 退出条件: 最小实现、最小验证和 write-back 已完整落盘，自动流可继续进入下一个审计/phase 节点。
- 退出条件: supervisor / loop / pending session 不再处于 stale、storm 或 provider blocked 的异常回路。
- 退出条件: 负责人判断、read pack、handoff 与 recovery state 口径一致，不再互相冲突。

## Target Phase

- `phase_number = 8`
- `phase_title = Phase 1 MVP Recovery`

## Target Phase Definition

## Phase 8: Phase 1 MVP Recovery

目标：

- 回到 `05-开发Phase里程碑与验收.md` 的产品主线，优先补齐 Web + App MVP 内测缺口。

建议范围：

1. 收口 `clients/web`、`clients/android`、`clients/ios` 的 MVP 核心链路和通知/深链/状态一致性缺口。
2. 为 Web/App 的登录、房间、投喂、记录、个人中心形成统一验收路径。
3. 把真实房间连续运行、跨端一致性、弱网/前后台恢复等验证入口制度化。

退出条件：

- 至少一条 Web/App MVP 关键链路具备跨端一致性证据。
- 至少形成一套可重复执行的 MVP 验收入口或 runbook。

## Implementation Entry

- `target_web_root = clients/web/`
- `target_android_root = clients/android/`
- `target_ios_root = clients/ios/`
- `target_web_root_exists = yes`
- `target_android_root_exists = yes`
- `target_ios_root_exists = yes`
- 当前目标是补齐 Phase 1 MVP 缺口：优先推进 Web/App 的登录、房间、个人中心、通知/深链或跨端一致性中的真实主链路，并沉淀 MVP smoke 入口。
- `required_first_write_paths = `clients/web/src/app/page.tsx`, `clients/web/src/app/rooms/[id]/page.tsx`, `clients/web/src/app/me/page.tsx`, `clients/android/app/src/main/java/live/yunmao/app/MainActivity.kt`, `clients/android/app/src/main/java/live/yunmao/app/ui/NavGraph.kt`, `clients/ios/YunmaoApp/Sources/YunmaoApp/Views/RoomListView.swift`, `clients/ios/YunmaoApp/Sources/YunmaoApp/Views/RoomDetailView.swift`, `docs/dev/runbooks/mvp-smoke.md``
- `allowed_reference_paths = clients/web, clients/android, clients/ios, go/services, docs/finalproductplanning, reports/project_assessment_20260528`
- 若无法立即推进 MVP 主链路，必须直接写回 `reports/work_report_iteration_299.md` 记录阻塞，不要继续回退到历史 phase 或虚构 `repo/apps/app`

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在当前 phase 指定的 `clients/`、`go/`、`rust/`、`deploy/`、`scripts/`、`.github/workflows/` 与 `docs/` 路径推进实现，不允许回退到不存在的 `repo/...` 模板。
- 路径基准必须固定为 workspace 根目录：源码位于 `clients/`、`go/`、`proto/` 等目录，规则/报告/交接文档位于 workspace 根目录，禁止虚构 `repo/...` 前缀。
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_299.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
