# Dispatch Prompt: Owner Closeout Review

请作为 `yunmao` Owner + QA 联合负责人，执行最终一轮 closeout 复核。

## 任务目标

- 在支付/IAP、TURN/RTC、Rust/JWT 三路 closeout 完成后，给出一份新的 owner 决策
- 明确项目下一步是开启新 phase，还是以 closeout 方式归档当前主线

## 必读输入

- `docs/execution/20260529-yunmao-phase6-planning/01-CURRENT-STATUS-SUMMARY.md`
- `docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md`
- `docs/execution/20260529-yunmao-phase6-planning/06-DISPATCH-INDEX.md`
- `docs/execution/20260529-yunmao-phase6-planning/07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md`
- `docs/execution/20260529-yunmao-phase6-planning/08-DISPATCH-TURN-RTC-CLOSEOUT.md`
- `docs/execution/20260529-yunmao-phase6-planning/09-DISPATCH-RUST-JWT-CLOSEOUT.md`
- `reports/codemaster/project_owner_brief.md`
- `reports/codemaster/run_summary_yunmao_owner_planning_refresh_20260529.json`

## 前置条件

- 三路 closeout 都已输出新 evidence
- 仍未关闭的 blocker 都有最新 owner 和原因
- 最新治理刷新结果已可读取

## 明确禁止

- 不在证据不足时给出“已收口”结论
- 不用口头承诺替代 evidence path
- 不把外部输入缺失包装成“项目已 ready”

## 必做事项

1. 逐项复核剩余 blocker：
   - 支付 / Apple IAP
   - TURN
   - Rust data-plane
   - JWT/JWKS
2. 复核新治理刷新是否仍是 `waiting_for_fix`，还是已经有更明确结论。
3. 明确当前项目应：
   - 开启新 phase
   - 继续 closeout
   - 或阶段归档
4. 输出新的 owner 决策与建议动作。

## 输出要求

- `owner_closeout_decision.md`
- `owner_closeout_decision.json`
- `blocker_closure_matrix.md`
- `next_action_recommendation.md`

## 完成判定

- 能明确回答当前应进入哪一种后续路径
- 若仍未收口，能明确指出剩余 blocker 与 owner
- 若建议开启新 phase，能说明依据与边界
