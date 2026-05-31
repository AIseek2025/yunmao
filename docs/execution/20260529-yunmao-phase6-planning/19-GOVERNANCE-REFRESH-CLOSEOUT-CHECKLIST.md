# yunmao Governance Refresh Closeout Checklist

## 用法

本清单用于在 owner closeout 结论形成后，判断是否需要刷新治理面，并核对刷新后的工件是否与最新 closeout 结论一致。

适合搭配 [17-RESULT-COLLECTION-CHECKLIST.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/17-RESULT-COLLECTION-CHECKLIST.md) 一起使用。

## 何时需要刷新

- `owner_closeout_decision.md` 已形成，且结论不再等同于当前治理工件中的状态
- 任一 blocker 的 `Status / Owner / Evidence / Next Action` 发生实质变化
- `project_owner_brief`、`run_summary`、`owner_escalation_report` 仍停留在旧结论
- 新增证据已经落盘，但治理面还没消费到

## 何时不要刷新

- 三路 write-back 仍未齐备
- owner closeout 还没有唯一结论
- 新结果只是聊天里的口头状态，没有文件化证据
- 本轮没有任何 blocker 或 owner 结论变化

## 刷新前检查

1. 已存在以下文件：
   - `payments_iap_writeback.md`
   - `turn_rtc_writeback.md`
   - `rust_jwt_writeback.md`
   - `owner_closeout_writeback.md`
   - `owner_closeout_decision.md`
   - `owner_closeout_decision.json`
2. `blocker_closure_matrix.md` 已覆盖 4 类 blocker。
3. 需要抬进治理面的最新 evidence path 都已在 write-back 中写明。
4. 当前想要刷新的目标结论已明确，例如：
   - 仍是 `waiting_for_fix`
   - 转成更明确的 closeout pending
   - 或已准备进入新 phase / 当前主线归档

如果还没有形成 owner 输出，可先参考：

- [20-OWNER-CLOSEOUT-OUTPUT-EXAMPLE-INDEX.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/20-OWNER-CLOSEOUT-OUTPUT-EXAMPLE-INDEX.md)

## 刷新命令模板

```bash
python3 scripts/refresh_governance_artifacts.py \
  --workspace-root /Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao \
  --trace-id <trace_id> \
  --task '<task_summary>' \
  --required-followup '<followup_summary>'
```

## 推荐命名

- `trace_id` 建议使用: `yunmao_owner_closeout_refresh_<YYYYMMDD>_<short_trace>`
- `task_summary` 建议写清:
  - 本轮是 closeout refresh
  - 触发原因是 owner closeout 结论更新
- `required_followup` 只保留真实仍未完成的 follow-up，避免把普通描述写成长期 pending

## 刷新后必查工件

1. [project_owner_brief.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/project_owner_brief.md)
2. [run_summary_yunmao_owner_planning_refresh_20260529.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/run_summary_yunmao_owner_planning_refresh_20260529.json)
3. [owner_escalation_report.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/owner_escalation_report.json)
4. [dashboard_summary.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/dashboard_summary.json)
5. 最新 `alert_event_*.json`

## 刷新后核对项

1. Owner 语义
   - `project_owner_brief` 是否仍与最新 owner closeout 决策一致
   - 是否错误回退成 `continue_development`
   - `dispatch_ready` 是否符合当前结论
2. Finish state
   - `run_summary` 的 `finish_state` 与 `finish_reason` 是否能解释当前 closeout 状态
   - 是否再次出现无依据的 `owner_state_conflict`
3. Owner consistency
   - `owner_consistency_status` 是否为 `ok`
   - `owner_consistency_conflicting_hints` 是否为空或可解释
4. 治理传播
   - `owner_escalation_report` 与 `dashboard_summary` 是否承接到同一结论
   - `alert_event` 是否只反映真实剩余 blocker，而不是旧状态残留

## 不通过信号

- `project_owner_brief` 与 `owner_closeout_decision.md` 结论冲突
- `run_summary` 出现新的 phase mismatch 或无意义 conflict
- 旧 blocker 已关闭，但治理面仍继续报旧 blocker 为主结论
- `required_followup` 写法导致治理面被误导为长期 `waiting_for_fix`

## 刷新后收口动作

1. 把最新治理工件路径补回 owner closeout 输出目录。
2. 在 `owner_closeout_writeback.md` 中补一行治理刷新结果摘要。
3. 如果治理面与 owner 结论一致，本轮 closeout 文档链即视为收口完成。
4. 如果治理面仍不一致，优先修正输入文案或 follow-up，再重新刷新，不要口头解释代替工件修正。
