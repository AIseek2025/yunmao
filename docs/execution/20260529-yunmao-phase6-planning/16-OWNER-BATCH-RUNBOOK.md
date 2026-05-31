# yunmao Owner Batch Runbook

## 目标

本手册把当前 packet 中已经定义好的 planning、dispatch、write-back 压缩成一套可直接照着执行的批次操作顺序。

适用场景：

- 负责人需要按最小批次发单，而不是自己重新拆任务
- 负责人需要知道每一路应该先读什么、产出什么、何时进入 owner closeout
- 负责人需要把结果统一收回到同一轮 closeout 判断中

## 批次总览

1. Batch 0 `prep_and_truth_source`
   - 阅读 [01-CURRENT-STATUS-SUMMARY.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/01-CURRENT-STATUS-SUMMARY.md)、[02-OWNER-PLANNING-HANDOFF.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/02-OWNER-PLANNING-HANDOFF.md)、[04-NEXT-PHASE-PLAN.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md)
   - 锁定本轮只处理 4 类 blocker: Payments/IAP、TURN/RTC、Rust data-plane、JWT/JWKS
   - 确认当前 owner 语义仍是 `planning / request_planning / dispatch_ready=false`
2. Batch 1 `payments_iap_closeout`
   - 发 [07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md)
   - 回收 [12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md) 对应结果
3. Batch 2 `runtime_parallel_closeout`
   - 并行发 [08-DISPATCH-TURN-RTC-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/08-DISPATCH-TURN-RTC-CLOSEOUT.md) 与 [09-DISPATCH-RUST-JWT-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/09-DISPATCH-RUST-JWT-CLOSEOUT.md)
   - 分别回收 [13-TEMPLATE-TURN-RTC-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/13-TEMPLATE-TURN-RTC-WRITEBACK.md) 与 [14-TEMPLATE-RUST-JWT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/14-TEMPLATE-RUST-JWT-WRITEBACK.md)
4. Batch 3 `owner_closeout_review`
   - 在前三路结果齐备后发 [10-DISPATCH-OWNER-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/10-DISPATCH-OWNER-CLOSEOUT.md)
   - 使用 [15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md) 输出最终结论
5. Batch 4 `governance_refresh_if_needed`
   - 若前三路或 owner 结论改变了 blocker 状态，再刷新治理面
   - 刷新后复核 `project_owner_brief`、`run_summary`、`owner_escalation_report` 是否与 owner closeout 结论一致

## 每批次输入与输出

| Batch | 主输入 | 主 dispatch | 主 write-back | 完成标准 |
|------|--------|-------------|----------------|----------|
| Batch 0 | `01/02/04` + `project_owner_brief` | 无 | 无 | 对齐本轮只做 closeout，不继续泛化开发 |
| Batch 1 | 支付/IAP 真实输入现状 | `07` | `12` | WeChat/Alipay/Apple IAP 输入状态可复核 |
| Batch 2A | TURN/RTC 当前 evidence | `08` | `13` | STUN-only 是否已推进到真实 TURN credentials |
| Batch 2B | Rust/JWT 当前 evidence | `09` | `14` | Rust data-plane 与 JWT/JWKS 运行态是否闭环 |
| Batch 3 | `12/13/14` 三路结果 | `10` | `15` | 给出 `continue_closeout | open_new_phase | archive_current_phase` |
| Batch 4 | 最新 closeout 结论 | 可选 | 可选 | 治理面与 owner closeout 结论一致 |

## 发单顺序

1. 先完成 Batch 0，避免把当前 planning packet 错发回开发态。
2. 先发 Payments/IAP，尽早暴露外部输入是否仍属 `external_wait`。
3. TURN/RTC 与 Rust/JWT 可并行，但都必须使用各自模板回写。
4. Owner closeout 只能在 12、13、14 三份结果齐备后执行。
5. 若 owner closeout 明确改变状态，再决定是否刷新治理产物。

## 命名规则

- dispatch 轮次标识建议使用: `YYYYMMDD-<track>-<short-trace>`
- track 建议固定为:
  - `payments-iap`
  - `turn-rtc`
  - `rust-jwt`
  - `owner-closeout`
- write-back 主文件建议命名:
  - `payments_iap_writeback.md`
  - `turn_rtc_writeback.md`
  - `rust_jwt_writeback.md`
  - `owner_closeout_writeback.md`
- 证据文件优先沿用各 dispatch 中已经定义的输出名，避免一轮一个别名。

## 回写落点

- 每一路执行结果应至少包含 1 份模板化 write-back + 该 dispatch 规定的证据文件。
- 建议把同一路结果收敛到单独目录，目录模式可用:
  - `reports/closeout/<YYYYMMDD>-payments-iap-<short-trace>/`
  - `reports/closeout/<YYYYMMDD>-turn-rtc-<short-trace>/`
  - `reports/closeout/<YYYYMMDD>-rust-jwt-<short-trace>/`
  - `reports/closeout/<YYYYMMDD>-owner-closeout-<short-trace>/`
- 如果执行方使用其他目录，也必须在对应 write-back 中写明最终 evidence path。

## 最终汇总路径

1. 单路结果先汇总到各自 write-back。
2. 三路技术/集成结果汇总到 [15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md) 对应的 owner closeout 实例。
3. 负责人最终结论应至少沉淀为：
   - `owner_closeout_decision.md`
   - `owner_closeout_decision.json`
   - `blocker_closure_matrix.md`
   - `next_action_recommendation.md`
4. 如本轮结果改变治理状态，再把新结论回写到治理刷新产物。

## 实例参考

- 可直接复用的批次命名、目录布局与发单正文骨架见 [18-OWNER-DISPATCH-EXAMPLE-PACKET.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/18-OWNER-DISPATCH-EXAMPLE-PACKET.md)
- 若 owner closeout 结论需要抬进治理面，按 [19-GOVERNANCE-REFRESH-CLOSEOUT-CHECKLIST.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/19-GOVERNANCE-REFRESH-CLOSEOUT-CHECKLIST.md) 执行刷新和复核

## 明确禁止

- 不跳过 Batch 0 直接把 `07-10` 当成泛化开发任务分发
- 不在未收到 `12/13/14` 的情况下提前执行 owner closeout
- 不把 process-level smoke 旧证据当成本轮 closeout 新证据
- 不在 write-back 缺失 evidence path 时宣称 blocker 已关闭
