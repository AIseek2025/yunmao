# ADR-0022 弹幕审核架构与撤回语义

- 状态：Accepted (第七轮)
- 上下文：chat-svc 第六轮已具备本地词表 + admin 审核动作；本轮收尾要求：
  1. 提供 `ModerationProvider` 抽象接入外部审核 SaaS（阿里云内容安全 / 腾讯天御 / 网易易盾）；
  2. 在外部凭据缺失或调用失败时自动降级到本地词表；
  3. 审核动作扩展支持 `recall`，并通过 `room.chat.moderation` 事件广播让客户端把已展示的消息撤回。

## 决策

### 1. Provider 抽象 + Manager fallback

- 新增 `services/chat-svc/internal/moderation`：
  - `Provider` interface：`Name()` + `Inspect(ctx, text) -> (Decision, error)`。
  - `LocalProvider`：内置词表 + `SetWords` 热更新。
  - `AliyunGreenProvider`：阿里云 SDK 占位（mock 模式，CI/dev 友好）；
    实际接 SDK 时只需替换 `Inspect` 中的 mock 逻辑，结构不变。
  - `Manager`：保存 primary + fallback；`Inspect` 走 primary，
    失败/超时（250ms）自动降级到 fallback；并记录 metrics：
    - `yunmao_chat_moderation_calls_total{provider, outcome}` outcome ∈ {ok, error, fallback, fallback_error}
    - `yunmao_chat_moderation_latency_seconds{provider}`
- chat-svc `Config.Filter` 通过 `service.ModerationManagerFilter` 适配为
  `SensitiveFilter` 接口（向后兼容现有发送路径）。

### 2. 配置 + 热切

- `YUNMAO_CHAT_MODERATION` ∈ {`local`, `aliyun_green`, `tencent_tms`, `easeshield`}；默认 `local`。
- 阿里云凭据：`YUNMAO_ALIYUN_AK / SK / REGION`；
  当 `YUNMAO_CHAT_MODERATION=aliyun_green` 且凭据缺失 → 自动回到 `local`（同时启动日志告警）。
- admin-svc feature flag `chat.moderation_provider`：
  - 后续可热切（chat-svc 监听 5min 缓存）。
  - 紧急情况见 runbook：`docs/dev/runbooks/chat-moderation-fallback.md`。

### 3. 动作扩展（含 recall）

`Action` 全集：

| Action | 客户端行为 | 服务端行为 |
| --- | --- | --- |
| `pass` | 正常展示 | 入库 + 扩散 message |
| `warn` | 加 ⚠ 标记 | 入库；事件 `room.chat.moderation{action:warn}` |
| `hide` | 替换为 `[已隐藏]` | 入库（`moderation_status=hidden`）；事件 |
| `recall` | 从消息列表移除（DOM 删除） | 入库 `moderation_status=recall`；事件 |
| `mute` | 提示「用户被禁言 N 分钟」 | 仅事件（不删消息）；mute 由 user-svc 落库 |
| `block` | 不展示 | 不入库；直接拒绝 send |

事件 payload：

```json
{
  "id": "msg_xxx",
  "action": "recall",
  "reason": "spam",
  "ts": "2026-05-25T10:00:00Z"
}
```

### 4. 客户端实现（web-demo）

- WS 帧增加 `op=event, type=room.chat.message` 与 `type=room.chat.moderation` 处理；
- 渲染 chat 行时附 `data-msg-id`，moderation 帧到达后按 action 操作对应行。

## 备选与拒绝

- 由 chat-svc 直接维护一份「黑名单 + 速率限制」并禁用外部审核：拒绝。
  无法满足合规要求；多家平台已经因关键词遗漏被处罚。
- 用同步 RPC 接外部审核（每条弹幕必经外部）：保留本方案；
  但加 250ms 超时 + 降级，避免外部抖动卡死全链。

## 后果

- 维护成本：需关注 4 家 SaaS 的 SDK 版本与配额（KMS/access key 轮换走 yunmao-secrets）；
- 风险：fallback 到 local 时漏检率上升，需在 dashboard 上展示
  `chat_moderation_calls{outcome="fallback"}` 比例并配告警；
- 测试：单测覆盖 LocalProvider 热更新、AliyunGreenProvider mock 三态、Manager fallback
  与 `Active()` 切换（见 `provider_test.go`）。
