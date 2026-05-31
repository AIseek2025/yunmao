# yunmao Phase 11 Candidate Plan

## 文档定位

- 类型: `candidate_phase_plan`
- 状态: `draft_only`
- 日期: `2026-05-31`
- 适用前提: `Phase 10` 已完成，负责人希望继续沿 `Phase 4` 产品主线 materialize 新阶段

本文档只定义 `Phase 11` 的治理候选方案，不代表该 phase 已 materialize，也不授权直接进入编码或 repair。只有在负责人明确接受本草案、并补齐新的 handoff / owner 入口后，`Phase 11` 才能成为新的 live target。

## 候选结论

- 候选 Phase: `Phase 11`
- 候选标题: `Phase 4 Platformization And Cross-Region Readiness`
- 一句话摘要: 在 `Phase 10` 已完成“最小多端增强 + Rust 收益验证 + 规模化材料归档”后，下一阶段若继续沿 `Phase 4` 主线推进，应聚焦多端统一能力、Rust 试点收益放大与跨区域/规模化 readiness，而不是回退到 closeout 或重复 Phase 10 的最小闭环。

## 为什么是这个候选 Phase

1. `docs/finalproductplanning/05-开发Phase里程碑与验收.md` 中 `Phase 4` 的高阶目标仍包括：小程序核心链路、App 深度能力、Rust 试点明确收益、多区域部署与容量规划。
2. 当前 `Phase 10` 的通过结论已经覆盖“至少一条多端增强真实落地 + 至少一条 Rust/规模化证据闭环”，但还没有把 `Phase 4` 的剩余高阶目标 materialize 成新的执行阶段。
3. 因此，若不选择 closeout，而是继续向前推进，最自然的下一阶段不是重做 `Phase 10`，而是把 `Phase 4` 中更高阶的 platformization / scale readiness 范围单独 materialize。

## 目标

- 把 `Phase 4` 剩余的高阶目标收敛成一个可以明确接管、明确验收、明确不纳入范围的新阶段。
- 避免把“是否继续推进”混成泛化开发，要求新阶段从一开始就具备清晰的边界、退出条件和治理入口。

## 纳入范围

1. 多端统一能力:
   - Web / App / 小程序使用统一账号、房间、投喂、支付、通知与设备 API 的差距梳理与最小闭环
   - 小程序核心链路候选范围：看直播、登录、投喂、分享
2. App 深度能力:
   - 播放 SDK、推送、画中画/后台策略、离线草稿与本地缓存中的至少一条真实增强链路
3. Rust 试点收益放大:
   - `gateway / media-edge / device-edge` 至少一条收益从“有材料”升级到“有明确对比收益”
   - 收益维度可以是延迟、成本、连接数、稳定性中的至少一项
4. 跨区域与规模化 readiness:
   - 多区域部署、容量规划、跨 region 演练、故障演练材料的最小执行链

## 不纳入范围

1. 仅为“补文档”而重新打开已经审计通过的 `Phase 10` 证据链
2. 与 `Phase 4` 高阶目标无关的泛技术债清理
3. 没有新的 owner 决策、handoff 和退出条件前直接开始编码
4. 把 closeout / archive 决策缺口伪装成新功能 phase

## 建议主入口

1. `docs/finalproductplanning/05-开发Phase里程碑与验收.md`
2. `docs/autopilot/02_phase_plan.md`
3. `docs/autopilot/08_phase10_completion_and_closeout_decision_2026-05-31.md`
4. `reports/work_report_iteration_349.md`
5. `reports/audit_report_iteration_349.md`

## 候选工作流

### Track A: 小程序与多端统一能力

- 目标: 把 `Phase 4` 中“小程序核心链路 + 多端统一 API”从产品愿景收敛成最小真实路径
- 建议入口: `clients/`、共享契约、通知/支付/设备 API
- 最小完成定义:
  - 至少一条小程序核心链路有明确实现或受控 blocker
  - 至少一条 Web/App/小程序 统一能力形成跨端对齐证据

### Track B: App 深度能力增强

- 目标: 选择一条 App 深度能力，不做大而全扩面
- 候选方向:
  - 更完善的播放 SDK
  - 厂商推送
  - 画中画 / 后台策略
  - 离线草稿 / 本地缓存
- 最小完成定义:
  - 至少一条能力形成真实代码与验证证据

### Track C: Rust 试点收益放大

- 目标: 把 Rust 路线从“有试点”推进到“收益可比较”
- 最小完成定义:
  - 形成一份基于前后对比的收益说明
  - 对比指标至少覆盖延迟、成本、连接数、稳定性之一

### Track D: Cross-Region And Scale Readiness

- 目标: 把“多区域部署和容量规划”从材料项推进到最小执行链
- 最小完成定义:
  - 至少一项跨 region / 故障演练 / 容量演练形成新的执行证据
  - 与现有回滚和监控口径保持一致

## 候选退出条件

1. 至少一条小程序或跨端统一能力具备真实实现或受控 blocker 结论。
2. 至少一条 App 深度能力具备真实代码与验证证据。
3. 至少一条 Rust 试点收益具备可比较的 before/after 证据。
4. 至少一项跨区域或规模化 readiness 演练形成可审计证据。

## Materialization Gate

只有在以下条件满足时，才建议把本草案升级成真正的 `Phase 11`：

1. 负责人明确选择 `open_new_phase`，而不是 `continue_closeout` 或 `archive_current_phase`
2. `project_owner_brief` / `project_owner_read_pack` / 新 handoff 三者已同步到新的 `Phase 11` 语义
3. 本文档对应的第一轮实现入口、禁止动作、验证口径已被写入新的 handoff
4. 可直接参考 `docs/autopilot/10_phase11_handoff_draft_2026-05-31.md` 作为正式 handoff 的草稿基线，但在改判前不得把它当作 live handoff

## 暂不 materialize 的情形

- closeout / archive 结论尚未收敛
- 新阶段只是为了绕开当前治理收口
- 新阶段边界与 `Phase 10` 高度重叠，不能说明为什么必须新开一轮

## 负责人备注

- 当前文档只是 `Phase 11` 候选方案，不替代当前 `continue_closeout` 结论。
- 若后续负责人继续选择 closeout，本文件应保留为备选治理输入，而不是自动提升为当前主目标。
- `10_phase11_handoff_draft_2026-05-31.md` 仅是 materialization 前草稿，不代表当前已经允许发车。
