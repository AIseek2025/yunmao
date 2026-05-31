# yunmao Write-Back Template Index

## 用法

本索引用于承接 [06-DISPATCH-INDEX.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/06-DISPATCH-INDEX.md) 的执行结果回写。

如果需要按批次回收结果、检查必收文件是否齐备，请配合：

- [16-OWNER-BATCH-RUNBOOK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/16-OWNER-BATCH-RUNBOOK.md)
- [17-RESULT-COLLECTION-CHECKLIST.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/17-RESULT-COLLECTION-CHECKLIST.md)

每一路 dispatch 完成后，应至少输出一份对应模板，统一记录：

- 当前结论
- 已完成事项
- 剩余 blocker
- 证据路径
- 下一步建议

## 模板列表

1. 支付与商店输入回写模板: [12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md)
2. TURN 与 RTC 回写模板: [13-TEMPLATE-TURN-RTC-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/13-TEMPLATE-TURN-RTC-WRITEBACK.md)
3. Rust/JWT 回写模板: [14-TEMPLATE-RUST-JWT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/14-TEMPLATE-RUST-JWT-WRITEBACK.md)
4. Owner closeout 回写模板: [15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md)

## 建议回写顺序

1. 各执行路先各自产出 12-14 对应模板
2. Owner 最后读取前三路结果，再使用 15 输出最终决策模板
