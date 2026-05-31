# Dispatch Prompt: Payments And IAP Closeout

请作为 `yunmao` 支付后端 + 业务 owner + 外部渠道对接联合负责人，执行一轮支付与商店输入 closeout。

## 任务目标

- 把当前 `credential diagnostics` 中的 `missing / partial` 状态推进到可复核的真实测试输入状态
- 关闭 WeChat Pay、Alipay、Apple IAP 相关外部输入 blocker

## 必读输入

- `docs/execution/20260529-yunmao-phase6-planning/01-CURRENT-STATUS-SUMMARY.md`
- `docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md`
- `docs/dev/runbooks/credential-cutover.md`
- `reports/work_report_iteration_296.md`
- `reports/iteration_296_evidence/credential-diagnostics.json`
- `reports/codemaster/project_owner_brief.md`

## 已知事实

- Phase 6 仓库内 process-level smoke 已完成并通过审计
- 当前支付与 Apple IAP 阻塞不是代码主线没做，而是测试输入尚未到位
- 当前不能把 mock / partial 状态误报为真实联调通过

## 明确禁止

- 不用 mock 配置冒充真实沙箱输入
- 不把 `partial` 解释成“可上线”
- 不跳过新的 diagnostics 复核

## 必做事项

1. 补齐 WeChat Pay 最小测试输入：
   - `YUNMAO_WECHAT_MCH_ID`
   - `YUNMAO_WECHAT_APIV3_KEY`
   - serial / client key / platform public key
2. 补齐 Alipay 最小测试输入：
   - `YUNMAO_ALIPAY_APP_ID`
   - 商户私钥
   - Alipay 公钥
3. 补齐 Apple IAP 最小测试输入：
   - `YUNMAO_APPLE_BUNDLE_ID`
   - Apple Root CA / trusted roots
   - 相关测试资料
4. 重新执行 credentials diagnostics，并输出新旧差异。
5. 明确哪些输入已经就绪，哪些仍是外部等待。

## 输出要求

- `summary.md`
- `payments_iap_input_matrix.md`
- `credential_diagnostics_after.json`
- `credential_diagnostics_delta.md`
- `external_waiting_items.md`

## 完成判定

- WeChat Pay / Alipay / Apple IAP 的测试输入状态可被清晰复核
- 新 diagnostics 不再以 `missing / partial` 为主结论
- 仍缺失的输入被明确写成 `external_wait`
