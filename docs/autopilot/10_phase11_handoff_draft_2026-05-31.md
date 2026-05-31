# Phase 11 Handoff Draft

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = draft_only_not_materialized`

## Read First Paths

1. `docs/autopilot/09_phase11_candidate_plan_2026-05-31.md`
2. `docs/autopilot/08_phase10_completion_and_closeout_decision_2026-05-31.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `reports/work_report_iteration_349.md`
5. `reports/audit_report_iteration_349.md`
6. `docs/autopilot/02_phase_plan.md`
7. `docs/finalproductplanning/05-开发Phase里程碑与验收.md`

## Promotion Basis

- 当前仅存在 `Phase 11` 候选草案，**尚未**形成正式 phase promotion。
- 上游事实来源：`iteration_349` 已审计通过，`Phase 10` 已完成，但当前有效结论仍是 `continue_closeout`。
- 本文档的作用是提供 materialization 前的 handoff 草稿，而不是触发新的 live phase。

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 当前必须先以 `closeout` 语义完成负责人判断；只有当 owner 明确改判 `open_new_phase` 时，才允许把本草稿升级成正式 handoff

负责人接管协议：
- `owner_action = request_planning`
- `takeover_mode = owner_replan`
- `owner_judgment = 当前 `Phase 11` 仅是候选草案，不代表项目已进入新 phase。负责人必须先确认 closeout / archive 结论为何不足，以及为什么需要 materialize 新阶段；只有在明确选择 `open_new_phase` 后，才允许依据本草稿补正式 handoff 并启动执行轮。`
- `restart_strategy = pause_relaunch_until_replan_is_materialized`
- 必做下一步: 先读 `project_owner_read_pack`、`08_phase10_completion_and_closeout_decision_2026-05-31.md` 与 `09_phase11_candidate_plan_2026-05-31.md`，确认当前仍以 `continue_closeout` 为默认结论。
- 必做下一步: 若负责人决定继续 closeout，则停止在本草稿继续展开，不得把 `draft` 误写成 live phase。
- 必做下一步: 若负责人决定 `open_new_phase`，则必须新建正式 `Phase 11` handoff、同步 owner brief / read pack / phase plan，再启动执行轮。
- 禁止动作: 禁止把本草稿直接当成已 materialize 的 phase handoff 发车。
- 禁止动作: 禁止在没有新的 owner 决策与正式 write-back 前直接进入编码或 repair。
- 禁止动作: 禁止用“已有 Phase 11 draft”掩盖当前 closeout 仍未正式收口的事实。
- 退出条件: 已形成唯一的 owner 决策，明确是继续 `closeout` 还是切换 `open_new_phase`。
- 退出条件: 若选择 `open_new_phase`，正式 handoff、owner brief、read pack 与 phase plan 已同步到 `Phase 11` 语义。
- 退出条件: 若选择继续 closeout，本草稿保持为候选输入，不再被误当成 live 入口。

## Target Phase

- `phase_number = 11`
- `phase_title = Phase 4 Platformization And Cross-Region Readiness`

## Target Phase Definition

### Phase 11: Phase 4 Platformization And Cross-Region Readiness

目标：

- 把 `Phase 4` 剩余的高阶目标收敛成一个明确的新阶段：多端统一能力、App 深度能力、Rust 试点收益放大，以及跨区域/规模化 readiness。

建议范围：

1. 小程序与多端统一能力：统一账号、房间、投喂、支付、通知与设备 API 的差距梳理与最小闭环。
2. App 深度能力：播放 SDK、推送、画中画/后台策略、离线草稿与本地缓存中的至少一条真实增强链路。
3. Rust 试点收益放大：`gateway / media-edge / device-edge` 至少一条收益形成 before/after 对比。
4. Cross-region / scale readiness：至少一项跨 region、故障演练或容量演练形成新的执行证据。

退出条件：

- 至少一条小程序或跨端统一能力具备真实实现或受控 blocker 结论。
- 至少一条 App 深度能力具备真实代码与验证证据。
- 至少一条 Rust 试点收益具备可比较的 before/after 证据。
- 至少一项跨区域或规模化 readiness 演练形成可审计证据。

## Current Gate

- `draft_only = yes`
- `ready_to_materialize = no`
- `materialization_blocker = owner still defaults to continue_closeout`
- `required_decision = open_new_phase`

## Implementation Entry

- `clients_root = clients/`
- `rust_root = rust/`
- `deploy_root = deploy/`
- `docs_root = docs/`
- 本草稿只定义未来可能的实现入口，不授权现在开始改动。
- `candidate_first_write_paths = clients/miniapp, clients/android, clients/ios, rust/crates/yunmao-gateway, rust/crates/yunmao-media-edge, deploy/observability, docs/dev/runbooks`
- 如未完成 materialization gate，只允许更新治理文档，不允许写入候选实现路径。

## Rule

- 必须先完成 owner 决策，再决定是否把本草稿升级成正式 handoff。
- 必须把 `continue_closeout` 视为当前默认结论，除非有明确的新 write-back 反转该结论。
- 必须保证 `project_owner_brief`、`project_owner_read_pack`、`02_phase_plan.md` 与正式 handoff 一致后，才允许新 phase 发车。
- 禁止把候选 phase 当成“已经批准”的开发任务。

## Required Write-Back

- 若选择继续 closeout：写回新的 owner decision / closeout write-back。
- 若选择 `open_new_phase`：写回新的正式 `Phase 11` handoff、owner brief、owner read pack 与 phase plan 增量。
- 注意：本文档本身是 `draft` 工件，不是执行轮的 `work_report` 输出路径。
