# 04-设备接入数据模型与 API 边界

## 1. 设备接入目标

设备系统要保证“可控、可审计、可限量、可扩展”：

- 可控：平台能按房间、设备、时间窗和健康规则控制是否允许投喂。
- 可审计：每条用户请求、设备指令、设备回执都可追踪。
- 可限量：设备永远不能被用户并发点击直接驱动多次出粮。
- 可扩展：支持家庭、猫咖、救助站、品牌硬件、自研硬件多种接入。

## 2. 硬件接入路径

| 路径 | 适用阶段 | 优点 | 风险 |
| --- | --- | --- | --- |
| 平台认证套件 | MVP/成长期 | 体验可控，调试成本低 | 硬件采购与运维成本高 |
| 成品设备改造 | MVP 试点 | 快速验证，成本较低 | 稳定性和合规风险需评估 |
| 厂商云云对接 | 成长期 | 扩张快，用户已有设备可接入 | 商务依赖和接口不可控 |
| 自研设备 SDK | 规模期 | 标准统一，生态可扩展 | 需要硬件团队和认证体系 |

MVP 建议：选择 1-2 套认证硬件，平台掌控固件、证书、MQTT 主题和设备回执，避免一开始接入过多品牌。

## 3. IoT 通信架构

```text
Go 投喂服务
  → Kafka/RocketMQ(feed.command.requested)
  → 设备指令消费者(按 device_id 分区串行)
  → EMQX/HiveMQ MQTT Broker
  → 投喂机/边缘代理
  → MQTT status/telemetry 回执
  → Go 设备服务聚合状态
  → WebSocket/通知服务同步 Web、App、房间与用户
```

MQTT 主题建议：

- `devices/{device_id}/commands`：平台下发指令，设备订阅。
- `devices/{device_id}/acks`：设备指令确认，平台订阅。
- `devices/{device_id}/telemetry`：设备遥测，平台订阅。
- `devices/{device_id}/events`：卡粮、缺粮、重启、离线恢复等事件。

设备必须使用 MQTT over TLS，一机一证，Broker ACL 限定只能访问自己的主题。

## 4. 投喂状态机

```text
created
  → rejected      # 未登录、冷却、房间关闭、健康上限、风控拒绝
  → accepted      # 服务端校验通过
  → queued        # 等待设备消费者处理
  → dispatched    # MQTT 指令已发布
  → acknowledged  # 设备收到指令
  → succeeded     # 设备执行成功
  → failed        # 设备失败、超时、卡粮、离线
  → compensated   # 支付/投喂券/积分补偿完成
```

关键规则：

- `feed_request_id` 是用户请求幂等键。
- `device_command_id` 是设备指令幂等键。
- 同一 `device_command_id` 设备只能执行一次，重复收到必须返回已处理结果。
- 设备执行超时后，平台不自动重发真实出粮指令，必须先查询设备状态，防止重复出粮。

## 5. 核心数据模型

### user

- `id`
- `phone_hash`
- `wechat_union_id`
- `app_push_enabled`
- `nickname`
- `avatar_url`
- `role`
- `risk_level`
- `created_at`

### cat

- `id`
- `display_name`
- `gender`
- `breed`
- `birth_date`
- `story`
- `owner_id`
- `welfare_profile_id`
- `status`

### room

- `id`
- `cat_id`
- `display_name`
- `description`
- `city`
- `visibility`
- `live_status`
- `feeding_status`
- `feed_cooldown_seconds`
- `no_feed_window_start`
- `no_feed_window_end`
- `created_at`

### device

- `id`
- `room_id`
- `device_type`
- `hardware_model`
- `firmware_version`
- `certificate_id`
- `online_status`
- `last_seen_at`
- `remaining_food_grams`
- `health_status`

### live_stream

- `id`
- `room_id`
- `push_url`
- `playback_profiles`
- `protocols`
- `status`
- `last_keyframe_at`
- `qoe_summary`

### feed_request

- `id`
- `room_id`
- `cat_id`
- `user_id`
- `device_id`
- `amount_grams`
- `status`
- `reject_reason`
- `idempotency_key`
- `created_at`
- `completed_at`

### device_command

- `id`
- `feed_request_id`
- `device_id`
- `command_type`
- `payload`
- `signature`
- `status`
- `sent_at`
- `ack_at`
- `result_at`

### room_event

- `id`
- `room_id`
- `event_type`
- `actor_id`
- `payload`
- `created_at`

### user_device_session

- `id`
- `user_id`
- `client_type`：web、ios、android、mini_program
- `push_token`
- `app_version`
- `os_version`
- `last_seen_at`
- `notification_opt_in`

### order

- `id`
- `user_id`
- `channel`：wechat、alipay、apple_iap、manual
- `biz_type`：feed_ticket、membership、sponsorship
- `amount`
- `status`
- `created_at`
- `paid_at`

## 6. API 边界

### 客户端 API

客户端 API 从首期就服务 Web/H5 与 App，不允许只依赖 Web cookie 或浏览器能力。Web 可以使用同源 Cookie 或 Token，App 使用 Bearer Token；所有写接口都必须有幂等键、统一错误码和可追踪请求 ID。

| API | 说明 |
| --- | --- |
| `GET /api/v1/rooms` | 房间列表，支持状态和标签筛选。 |
| `GET /api/v1/rooms/{room_id}` | 房间详情、直播地址、投喂状态。 |
| `POST /api/v1/feed-requests` | 创建投喂请求。 |
| `GET /api/v1/feed-requests/{id}` | 查询投喂状态。 |
| `GET /api/v1/cats/{cat_id}` | 猫咪主页。 |
| `GET /api/v1/users/me/feed-records` | 我的投喂记录。 |
| `POST /api/v1/rooms/{room_id}/messages` | 发送弹幕/聊天消息。 |
| `POST /api/v1/auth/login/sms` | 手机号验证码登录，返回多端会话。 |
| `POST /api/v1/client-sessions` | 绑定 Web/App 客户端会话、App 推送 token 和设备信息。 |
| `GET /api/v1/notifications` | 查询站内消息和通知历史。 |
| `PATCH /api/v1/notification-settings` | 修改开播、可投喂、投喂结果等通知偏好。 |
| `POST /api/v1/orders` | 创建投喂券、会员或赞助订单；首期可灰度关闭支付入口，但 API 边界先固定。 |
| `GET /api/v1/orders/{order_id}` | 查询订单、权益和补偿状态。 |

### 管理 API

| API | 说明 |
| --- | --- |
| `POST /api/v1/host/applications` | 猫主人/机构入驻申请。 |
| `POST /api/v1/admin/rooms/{room_id}/feeding-status` | 开关投喂。 |
| `PATCH /api/v1/admin/rooms/{room_id}/feeding-policy` | 修改冷却和禁投喂时间窗。 |
| `POST /api/v1/admin/devices/{device_id}/bind` | 绑定设备。 |
| `POST /api/v1/admin/devices/{device_id}/commands/test` | 测试设备指令。 |

### 内部 gRPC

- `FeedingService.ValidateAndCreate`
- `DeviceControlService.DispatchCommand`
- `DeviceStateService.ReportTelemetry`
- `RoomRealtimeService.PublishEvent`
- `MediaControlService.GetPlaybackProfile`
- `NotificationService.Dispatch`
- `PaymentService.CreateOrder`
- `RiskService.CheckAction`

## 7. 指令 Payload 示例

```json
{
  "version": "1.0",
  "device_command_id": "cmd_01J...",
  "feed_request_id": "feed_01J...",
  "command": "dispense",
  "params": {
    "amount_grams": 5,
    "motor_duration_ms": 1200
  },
  "expires_at": "2026-05-25T04:00:30Z",
  "nonce": "r4nd0m",
  "signature": "hmac-sha256..."
}
```

设备回执示例：

```json
{
  "version": "1.0",
  "device_command_id": "cmd_01J...",
  "status": "succeeded",
  "actual_amount_grams": 5,
  "remaining_food_grams": 830,
  "executed_at": "2026-05-25T04:00:12Z",
  "errors": []
}
```

## 8. 动物福利规则

投喂服务必须在设备指令前校验：

- 房间是否允许投喂。
- 当前是否在禁投喂时间窗。
- 房间全局冷却是否结束。
- 用户个人冷却是否结束。
- 单猫每日主食/零食上限是否达到。
- 碗内余粮或称重传感器是否超过阈值。
- 猫主人/机构是否手动暂停。

规则必须支持运营后台调整，并记录调整人和原因。

## 9. 设备异常处理

| 异常 | 平台行为 |
| --- | --- |
| 设备离线 | 关闭投喂，房间展示离线，通知猫主人。 |
| 卡粮 | 关闭投喂，触发告警，禁止自动重试出粮。 |
| 粮量不足 | 允许运营配置是否继续投喂，默认关闭付费投喂。 |
| 摄像头在线但投喂机离线 | 保留直播，关闭投喂。 |
| 投喂机在线但直播离线 | 默认关闭投喂，避免用户看不到结果。 |
| 回执超时 | 标记失败或待确认，人工/设备状态二次校验后补偿。 |

## 10. 事件总线契约

### 10.1 Topic / Schema 治理

- 使用 Kafka（或 RocketMQ）作为唯一事件总线；topic 命名 `{domain}.{entity}.{event}`，例如 `feed.command.requested`、`device.state.changed`、`live.stream.online`。
- 所有事件统一封装为 **CloudEvents 1.0** 或自定义版本化信封：
  - `id`：UUID，幂等键
  - `source`：服务名 + 实例 ID
  - `type`：topic 一致
  - `time`：UTC ISO-8601
  - `subject`：业务主键（如 `room_id`、`device_id`、`feed_request_id`）
  - `dataschema`：Schema 注册中心地址 + 版本
  - `data`：Protobuf 序列化负载
- Schema 在独立的 schema-registry 中按 `topic@version` 注册；上线前必须经契约测试（参见 09 第 6 节）。
- 兼容策略：默认向前兼容（只能新增可选字段）；破坏性变更必须升 major 并并行发布旧版本至少 1 个迭代。

### 10.2 核心事件清单

| Topic | 触发方 | 主消费方 | 用途 |
| --- | --- | --- | --- |
| `user.account.registered` | user-service | analytics、growth | 漏斗、激活 |
| `user.session.bound` | user-service | notification、analytics | 多端会话绑定 |
| `room.state.changed` | room-service | realtime-gateway、analytics | 房间状态广播 |
| `live.stream.online` / `live.stream.offline` | media-edge / room-service | room-service、notification | 直播态变更触发关注通知 |
| `feed.request.created` | feeding-service | device-control、analytics、risk | 投喂主链路 |
| `feed.command.requested` | feeding-service | device-control | 进入设备指令队列（按 device_id 分区） |
| `feed.command.dispatched` | device-control | feeding-service、realtime-gateway | 同步 UI 状态 |
| `feed.command.acked` | device-state | feeding-service、notification、media-processor | 完成态广播 + 截图任务 |
| `device.state.changed` | device-state | room-service、notification | 设备在线/离线/粮量 |
| `device.telemetry` | device-state | analytics | 长期数据（冷链处理） |
| `chat.message.posted` | realtime-gateway | moderation、analytics | 弹幕审核与统计 |
| `notification.dispatched` | notification | analytics | 推送送达分析 |
| `payment.order.paid` | payment | feeding（权益）、analytics | 支付完成 |
| `risk.action.flagged` | risk | feeding、user、payment | 风控动作 |
| `media.qoe.session` | client | analytics | 客户端 QoE 上报（经网关） |

### 10.3 分区与顺序

- `feed.command.*`：按 `device_id` 取模分区，保证同一设备串行消费。
- `chat.message.posted`：按 `room_id` 取模分区，便于房间维度审核与广播一致性。
- `device.telemetry`：按 `device_id` 分区，吞吐优先；不要求严格顺序。
- 其余按 entity 主键分区即可。

### 10.4 死信与重放

- 每个 topic 配套 `*.dlq`；消费者重试 3 次失败后丢入 DLQ，由运营后台触发重放或人工处理。
- 关键链路（feed、payment、device）必须有“按时间窗 / 按 ID 集合”两种重放工具。

## 11. 错误码体系

统一错误响应：

```json
{
  "error": {
    "code": "FEED.COOLDOWN_NOT_FINISHED",
    "message": "投喂冷却中,剩余 42 秒",
    "remediation": "请稍后再试",
    "trace_id": "01J..."
  }
}
```

错误码命名：`{DOMAIN}.{REASON}`，大写蛇形。常见 domain：`AUTH`、`USER`、`ROOM`、`FEED`、`DEVICE`、`MEDIA`、`PAY`、`RISK`、`SYSTEM`。

| 类别 | 示例 | HTTP | 客户端策略 |
| --- | --- | --- | --- |
| 鉴权 | `AUTH.LOGIN_REQUIRED` / `AUTH.TOKEN_EXPIRED` | 401 | 触发登录流程 |
| 权限 | `AUTH.FORBIDDEN` | 403 | 文案提示，不重试 |
| 限流 | `SYSTEM.RATE_LIMITED` | 429 | 按 `Retry-After` 退避 |
| 投喂 | `FEED.COOLDOWN_NOT_FINISHED` / `FEED.HEALTH_LIMIT_HIT` / `FEED.DEVICE_OFFLINE` / `FEED.NO_FEED_WINDOW` | 409 | 显示明确文案 |
| 设备 | `DEVICE.UNBOUND` / `DEVICE.ERROR_JAMMED` | 409 | 不允许重试出粮 |
| 媒体 | `MEDIA.STREAM_OFFLINE` / `MEDIA.PROFILE_UNAVAILABLE` | 503 | 进入维护态文案 |
| 支付 | `PAY.ORDER_PAID` / `PAY.AMOUNT_MISMATCH` | 409 | 触发对账 |
| 风控 | `RISK.ACTION_BLOCKED` | 403 | 不展示风控细节 |

完整错误码字典在 `08` 的契约仓库维护，并提供 i18n 文案表。

## 12. API 版本与兼容策略

- URL 段中包含 `v1`，重大不兼容变更需要 `v2`。
- 字段新增允许（可选 + 默认值）；字段删除/语义变更必须升版本并保留 6 个月双跑。
- 所有写接口必须支持 `Idempotency-Key`（推荐 ULID/UUIDv7），服务端按业务键 + 幂等键去重。
- 客户端必须发 `X-Client-Type`、`X-App-Version`、`X-Device-Id`、`X-Trace-Id`，用于灰度与排障。

## 13. 硬件供应商矩阵

| 类别 | 候选厂商/方案（仅作选型清单，不代表已合作） | 评估维度 |
| --- | --- | --- |
| 摄像头 | 萤石、小米、海康家用、品牌 PoE 摄像头 | RTSP 开放性、低照度、可云端固件、隐私 |
| 投喂机 | 小佩 Petkit、霍曼 Homerun、CATLINK、咪贝乐等 | API 开放性 / 是否支持自有协议、出粮精度、卡粮检测 |
| 智能称重 | 第三方蓝牙/Wi-Fi 称重模块 | 精度、采样率、电池续航 |
| 边缘盒子 | 树莓派 4/5、RK35xx、Jetson Nano、定制工控板 | 算力、网络、IO、价格、量产周期 |
| MQTT 平台 | EMQX（自建/Cloud）、HiveMQ、阿里云 IoT 平台 | 单机连接数、ACL、规则引擎、灰度 |
| 推流 SDK | 云厂商 SDK、自研基于 GStreamer/FFmpeg/libsrt | 接入成本、兼容性、可观测性 |
| OTA | 自研基于差分包 + 签名校验 | 安全、回滚、灰度 |

MVP 选型原则：选 1–2 套“摄像头 + 投喂机 + 边缘盒子”组合，平台自己掌控固件与协议；其余先做技术评估，不并行接入。

## 14. 设备生命周期

```text
出厂 → 入库 → 出货 → 入驻申请 → 绑定房间 → 在线运营
   ↘ 维修/回收 → 重置 → 重新出货
```

每一步必须有审计记录：

- 出厂：写入唯一硬件 ID、密钥、固件版本；密钥不出厂存仓库。
- 入驻：用户提交申请后，运营在后台触发“激活” → 设备首启拉取证书。
- 绑定：设备只能绑定一个房间，解绑需运营/猫主人双方确认。
- 重置：解绑后必须远程下发 wipe，确保旧密钥失效。

## 15. 推流地址与设备签名规范

- 推流 URL：`rtmp(s)://push.yunmao.live/live/{room_id}?token=...&exp=...&sign=...`，签名按 `HMAC_SHA256(secret, room_id|exp|nonce)`。
- 播放鉴权：客户端请求播放配置时，服务端按用户/房间/角色签发短期 token（默认 60s–5min），CDN/边缘节点验证。
- 设备指令签名：参见 7 节 payload，HMAC + 设备私钥 + nonce + 过期；MQTT 包级再做一次时间窗校验。
- 所有密钥分级管理，存放在 KMS / Vault；服务端代码不可直接持有长生命周期密钥。
