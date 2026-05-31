# Example: Next Action Recommendation

## 当前建议路径

- 建议路径: `continue_closeout`
- 推荐理由: 剩余问题仍然围绕真实外部输入、真实 TURN 凭据、Rust data-plane 运行态和 JWT/JWKS 联调闭环，不属于新功能 phase 的起点。

## 立即动作

1. 维持当前 closeout packet，不切回泛化开发态。
2. 对 Payments / IAP 保持 `external_wait`，直到真实沙箱资料到位。
3. 对 TURN / RTC 保持 `infra_blocked`，直到出现新的 TURN credentials evidence。
4. 对 Rust data-plane 与 JWT / JWKS 保持 `runtime_blocked`，直到出现新的 staging 运行和跨服务证据。

## 72 小时内建议

1. 检查 `payments_iap_writeback.md` 是否已写明缺失的外部输入清单和 owner。
2. 检查 `turn_rtc_writeback.md` 是否已解释当前仍是 STUN-only 还是已形成 TURN credentials。
3. 检查 `rust_jwt_writeback.md` 是否已给出 gateway/device-edge/media-edge 的新运行证据。
4. 收齐三路结果后，重新生成 `owner_closeout_decision.md` 和 `blocker_closure_matrix.md`。

## 为什么不是另外两条路径

- 不是 `open_new_phase`
  - 现阶段不是缺少新目标，而是缺少现有 blocker 的最终收口证据。
  - 现在开启新 phase 会削弱 closeout 边界。
- 不是 `archive_current_phase`
  - 当前仍不能把 blocker 关闭状态写成 fully archived。
  - 提前归档会让治理工件和真实状态产生偏差。

## 治理刷新建议

- 如果 owner closeout 结论仍为 `continue_closeout`，治理刷新时应明确保留 closeout blocker 语义。
- 如果某一路 blocker 已真正关闭，刷新后应确认 `project_owner_brief` 与 `run_summary` 已反映最新变化。
- 如果刷新后仍出现 owner consistency 冲突，应先修正文案或 follow-up，再重新刷新。

## 最终交付

- `owner_closeout_decision.md`
- `owner_closeout_decision.json`
- `blocker_closure_matrix.md`
- `next_action_recommendation.md`
