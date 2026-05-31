# yunmao Phase Handoff Iteration 1

## Target Phase

- `phase_number = 1`
- `phase_title = Contract And CI Hardening`

## Target Phase Definition

### Phase 1: Contract And CI Hardening
- 以第八轮交付后的仓库状态为基线，优先收口“共享契约输出”和“自动化门禁”两类风险。
- 必须优先选择不依赖外部正式凭据的最小真实切口，避免首轮就被支付、审核、TURN 生产信息阻塞。
- 允许从 OpenAPI/shared schema、Android CI、webrtc/TURN 自动化验证三条线中选一条最可落地的主线推进，但不能首轮全部同时展开。

## Read First Paths

1. `docs/autopilot/01_project_charter.md`
2. `docs/autopilot/02_phase_plan.md`
3. `docs/autopilot/03_audit_checklist.md`
4. `docs/autopilot/07_rust_core_go_assist_architecture.md`
5. `docs/dev/07-seventh-iteration-deliverable.md`
6. `docs/dev/08-eighth-iteration-deliverable.md`
7. `docs/finalproductplanning/05-开发Phase里程碑与验收.md`
8. `docs/finalproductplanning/09-测试质量与发布工程.md`

## Goal

从第八轮之后的真实仓库状态起跑，选择一个**不依赖外部正式凭据**、但能显著降低后续多端并行开发风险的收口点，完成最小真实推进。

## Preferred First Slice

优先顺位：

1. `OpenAPI / 共享 schema 输出 + 至少一个客户端消费链路`
2. `Android CI 构建门禁`
3. `webrtc / TURN 自动化验证门禁`

若第一顺位在当前仓库结构下明显不可落地，允许切换到第二或第三顺位，但必须在 `work_report` 中说明原因。

## Rules

- 只允许基于磁盘上的最新状态开发，不允许忽略 `08-eighth-iteration-deliverable.md`。
- 第一轮不要同时开多个松耦合子系统；必须先完成一条最小可证明链路。
- 不允许把外部凭据缺失伪装成“代码已完全可生产运行”。
- 若修改契约或客户端类型，同步写明受影响客户端和验证方式。
- 若修改 CI / workflow / scripts，必须给出实际命令或 CI 证据。

## Required Write-Back

- `reports/work_report_iteration_1.md`

## Work Report Minimum Requirements

- 本轮实际修改的文件列表
- 本轮完成的最小目标
- 本轮验证命令与结果
- 未解决 blocker
- 对下一轮的单一建议
