# Phase Handoff Iteration 339

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_338.md`
2. `reports/audit_report_iteration_338.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_338` 已审计通过，可进入 `Phase 9`
- 审计来源：`reports/audit_report_iteration_338.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转


负责人接管协议：
- `owner_action = request_planning`
- `takeover_mode = owner_replan`
- `owner_judgment = 负责人判断当前阻塞已转成规划问题，先请求 GLM-4.5 负责人更新规划，再继续编码。 当前规划缺口: 当前已定义 phases 已全部完成；继续推进前需先做归档或下一 phase 规划。`
- `restart_strategy = pause_relaunch_until_replan_is_materialized`
- 必做下一步: 先按负责人判断执行 `request_planning`，推荐入口 `chat`。
- 必做下一步: 先读 `project_owner_read_pack` 与 handoff，再进入写入、验证或恢复动作。
- 必做下一步: 优先解决 `当前已定义 phases 已全部完成；继续推进前需先做归档或下一 phase 规划。` 并把结论写回新的报告工件。
- 禁止动作: 禁止跳过负责人必读包后直接盲目编码或重复全量读仓库。
- 禁止动作: 禁止在同一问题已进入 relaunch storm / repeated repair 后继续沿用旧 prompt 空转。
- 禁止动作: 禁止在 owner_replan / phase_alignment / request_audit 场景下继续把问题伪装成“还能直接开发”。
- 退出条件: 新的 planning / phase alignment / audit 结论已落盘，并且自动流下一步入口与负责人判断一致。
- 退出条件: supervisor / loop / pending session 不再处于 stale、storm 或 provider blocked 的异常回路。
- 退出条件: 负责人判断、read pack、handoff 与 recovery state 口径一致，不再互相冲突。

## Target Phase

- `phase_number = 9`
- `phase_title = Phase 2-3 Productization Recovery`

## Target Phase Definition

## Phase 9: Phase 2-3 Productization Recovery

目标：

- 继续补齐增长、运营、商业化与机构接入的核心缺口，而不是只停留在“支付准备度”。

建议范围：

1. 推进增长玩法、分享归因、排行榜、通知频控与运营后台闭环。
2. 推进支付回调、订单/退款/对账、权益一致性、设备健康与基础风控。
3. 为机构入驻、公益记录、财务导出等能力补齐最小可用路径或显式 blocker。

退出条件：

- 至少一条增长/运营能力和一条商业化/权益能力具备真实代码闭环。
- 至少一条支付或对账链路具备真实受控验证证据。

## Implementation Entry

- `billing_reference_root = go/services/billing-svc/`
- `billing_reference_root_exists = yes`
- `target_admin_root = clients/admin/`
- `target_android_root = clients/android/`
- `target_ios_root = clients/ios/`
- 当前目标是继续补齐增长/运营/商业化缺口：优先推进支付、对账、权益一致性或钱包/客户端支付入口中的最小真实闭环，并沉淀受控验证 checklist。
- `required_first_write_paths = `go/services/billing-svc/internal/pay/wechat.go`, `go/services/billing-svc/internal/pay/alipay.go`, `go/services/billing-svc/internal/pay/appleiap.go`, `go/services/billing-svc/internal/pay/reconcile.go`, `clients/admin/src/app/wallet/page.tsx`, `clients/android/app/src/main/java/live/yunmao/app/pay/PayManager.kt`, `clients/ios/YunmaoApp/Sources/YunmaoApp/Payments/StoreKitManager.swift`, `docs/dev/runbooks/billing-staging-checklist.md``
- `allowed_reference_paths = go/services/billing-svc, clients/admin, clients/android, clients/ios, go/services/device-svc, go/services/feeding-svc, docs/finalproductplanning`
- 若无法立即推进增长/商业化主链路，必须直接写回 `reports/work_report_iteration_339.md` 记录阻塞，不要把准备度文档误写成“功能已完成”

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在当前 phase 指定的 `clients/`、`go/`、`rust/`、`deploy/`、`scripts/`、`.github/workflows/` 与 `docs/` 路径推进实现，不允许回退到不存在的 `repo/...` 模板。
- 路径基准必须固定为 workspace 根目录：源码位于 `clients/`、`go/`、`proto/` 等目录，规则/报告/交接文档位于 workspace 根目录，禁止虚构 `repo/...` 前缀。
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_339.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
