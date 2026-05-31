# yunmao Next Phase Dispatch Index

## 用法

本索引对应 [04-NEXT-PHASE-PLAN.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md) 的下一轮最小发单包。

本轮不再按全仓库平铺发单，而是按 Phase 6 之后仍剩余的外部联调与 closeout blocker 拆成 4 路最小任务。

如果需要按批次执行而不是逐页自己拼装，请先看：

- [16-OWNER-BATCH-RUNBOOK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/16-OWNER-BATCH-RUNBOOK.md)
- [17-RESULT-COLLECTION-CHECKLIST.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/17-RESULT-COLLECTION-CHECKLIST.md)

## Dispatch 列表

1. 支付与商店输入 closeout: [07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md)
2. TURN 与 RTC closeout: [08-DISPATCH-TURN-RTC-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/08-DISPATCH-TURN-RTC-CLOSEOUT.md)
3. Rust data-plane 与 JWT/JWKS closeout: [09-DISPATCH-RUST-JWT-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/09-DISPATCH-RUST-JWT-CLOSEOUT.md)
4. 最终 owner closeout 复核: [10-DISPATCH-OWNER-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/10-DISPATCH-OWNER-CLOSEOUT.md)

## 对应回写模板

- 模板总索引: [11-WRITEBACK-TEMPLATE-INDEX.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/11-WRITEBACK-TEMPLATE-INDEX.md)
- 支付与商店输入回写: [12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md)
- TURN 与 RTC 回写: [13-TEMPLATE-TURN-RTC-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/13-TEMPLATE-TURN-RTC-WRITEBACK.md)
- Rust/JWT 回写: [14-TEMPLATE-RUST-JWT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/14-TEMPLATE-RUST-JWT-WRITEBACK.md)
- Owner closeout 回写: [15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md)

## 共用真相源

- [01-CURRENT-STATUS-SUMMARY.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/01-CURRENT-STATUS-SUMMARY.md)
- [02-OWNER-PLANNING-HANDOFF.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/02-OWNER-PLANNING-HANDOFF.md)
- [04-NEXT-PHASE-PLAN.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md)
- [project_owner_brief.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/project_owner_brief.md)
- [run_summary_yunmao_owner_planning_refresh_20260529.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/run_summary_yunmao_owner_planning_refresh_20260529.json)

## 建议执行顺序

1. 先发支付与商店输入 closeout
2. 再并行发 TURN/RTC closeout 与 Rust/JWT closeout
3. 三路完成后，最后发 owner closeout 复核

## 明确禁止

- 不得把已有 process-level smoke 冒充为真实外部联调闭环
- 不得借 closeout 发单继续扩功能
- 不得在缺少真实外部输入时把状态误写成 `Go`
