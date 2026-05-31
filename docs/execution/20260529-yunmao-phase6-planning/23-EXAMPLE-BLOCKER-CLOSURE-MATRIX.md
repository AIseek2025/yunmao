# Example: Blocker Closure Matrix

## 用法

本示例用于演示 owner 在最终 closeout 时，如何把 4 类 blocker 统一落成一张可复核的矩阵。

## Blocker Closure Matrix

| Blocker | Status | Owner | Evidence | Next Action |
|---------|--------|-------|----------|-------------|
| Payments / IAP | `external_wait` | `payments_owner` | `reports/closeout/20260529-payments-iap-round1/payments_iap_writeback.md` | 等待真实 WeChat/Alipay/Apple IAP 沙箱资料，再复跑 diagnostics |
| TURN / RTC | `infra_blocked` | `rtc_infra_owner` | `reports/closeout/20260529-turn-rtc-round1/turn_credential_evidence.md` | 提供 TURN shared secret 或托管 TURN infra，再复查 `ice_servers_after.json` |
| Rust data-plane | `runtime_blocked` | `platform_owner` | `reports/closeout/20260529-rust-jwt-round1/rust_dataplane_runtime_matrix.md` | 产出新的 gateway/device-edge/media-edge staging 启动证据 |
| JWT / JWKS | `runtime_blocked` | `auth_gateway_owner` | `reports/closeout/20260529-rust-jwt-round1/jwt_jwks_alignment.md` | 明确 issuer/verifier/JWKS 路径并复跑跨服务 smoke |

## 读表规则

- `Status` 只写当前真实状态，不用 optimistic 文案替代。
- `Owner` 必须是当前仍对 blocker 关闭负责的人或角色。
- `Evidence` 必须指向本轮最新文件，而不是旧 iteration 证据。
- `Next Action` 必须是单步可执行动作，而不是泛泛而谈的“继续跟进”。

## 不通过示例

- 用 `done` 覆盖 `external_wait` 或 `infra_blocked`
- `Evidence` 只写“见日志”而没有具体路径
- `Owner` 空白或只写“团队”
- `Next Action` 无法被下一位 owner 直接执行
