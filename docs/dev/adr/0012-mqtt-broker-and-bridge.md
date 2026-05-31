# ADR-0012：MQTT Broker 选型与 device-svc 桥接策略

- **日期**：2026-05-25
- **状态**：已采纳（MVP）
- **关联**：ADR-0003（事件总线 Kafka）、ADR-0007（投喂异步队列化）、ADR-0010（事务性 outbox）
- **作者**：架构组

## 上下文

第二轮把投喂事件统一到 Kafka（feed.command.* / device.state.*），但 device-svc 与真实设备之间仍是 mock HTTP。设备硬件、家用网络与平台之间存在三个特征：

1. **设备端是 IoT 资源受限节点**：内存 / CPU / 网络较弱，长连接保活成本敏感。
2. **下行命令必须严格按 device_id 串行**：动物福利 ADR-007 要求“防止重复出粮”。
3. **上行事件量大但单条小**：心跳 / 故障 / 出粮 ack / 余粮 telemetry 等高频小报文。

Kafka 直连家用 IoT 设备并不可行（端口、客户端、安全模型不匹配）。需要一个面向 IoT 的“最后一公里”协议。

## 决策

1. **协议**：采用 MQTT v3.1.1（兼容性最广，EMQX 默认行为稳定），保留 v5 升级路径。
2. **Broker**：MVP 自建 **EMQX 5**（开源单节点起步），同时兼容 NanoMQ / HiveMQ Community / 云 IoT。
   - 选 EMQX 的理由：单节点性能足够 50k 连接基线，Dashboard / ACL / WebHook 开箱即用，与中国云厂商集群方案兼容（EMQX Enterprise）。
   - NanoMQ 留作“嵌入式 / 边缘节点”备选；本轮不引入。
3. **拓扑**：device-svc 同时作为 Kafka 消费者 + MQTT 发布订阅者，扮演**协议网关**角色。
   - 下行：订阅 Kafka `feed.command.dispatched`（key=room_id）→ 拆出 device_id → 发布 `device/{device_id}/cmd/feed`（QoS1，保留为 false）。
   - 上行：订阅 `device/+/event/+`（QoS1）→ 翻译为 Kafka 事件（`device.state.changed` / `feed.command.acked` / `feed.command.completed`）。
4. **主题命名**：
   - 命令：`device/{device_id}/cmd/{cmd_type}`（cmd_type ∈ {feed, reboot, ota, config}）。
   - 上行：`device/{device_id}/event/{event_type}`（event_type ∈ {online, offline, heartbeat, feed_ack, feed_done, error, telemetry}）。
5. **认证**：
   - MVP 阶段 EMQX 启用 **`allow_anonymous=true`** 仅供 dev compose。
   - 生产：username = `device_id`，password = device-svc 使用 `KeyProvider` 颁发的短期凭证（HS256 占位；KMS 接入见 ADR-0014）。
   - 设备凭证 hash 落到 `devices.mqtt_credential_hash`（migration 0003）。
6. **ACL**：每个 device 只能 PUB 自己的 `device/{id}/event/*`、只能 SUB 自己的 `device/{id}/cmd/*`；通过 EMQX builtin ACL + `device_id` 模板实现。
7. **QoS 与幂等**：
   - 下行命令带 `device_command_id`，设备执行端用本地幂等表去重；后端 outbox 已是“至少一次”。
   - 上行 ack QoS1，device-svc 用 `mqttx` 兜底接收，转 Kafka 后由 feeding-svc 用 `Idempotent.Insert(ack, device_command_id)` 防重复扣减状态机。
8. **桥接实现**：见 `go/pkg/yunmao/mqttx`（统一 paho 适配 + 内存 broker 双实现）与 `go/services/device-svc/internal/bridge`（双向桥接器 + Prometheus 指标）。

## 替代方案

- **CoAP / HTTP 长轮询**：移动 / 弱网下保活成本高，工业经验少，废弃。
- **Kafka 直连设备**：客户端体积、Auth、网络要求都不适合家用 IoT，废弃。
- **gRPC streaming 双向**：服务端复杂度高，弱网恢复体验差，废弃。
- **AWS IoT Core / 阿里云 IoT**：托管方案稳定，但 MVP 需要本地可重现 + 成本可控；保留作为生产期备选。

## 影响

- 新增依赖：`github.com/eclipse/paho.mqtt.golang`（broker 端无新增二进制依赖，EMQX 通过 docker compose 启动）。
- 新增端口：1883（MQTT）、8083/8084/8883/18083。
- 失败模式：MQTT broker 不可达 → device-svc bridge 自动重连；命令暂停从 Kafka 出口流出，但 outbox 不会丢；恢复后继续处理。
- 部署：生产建议至少 3 节点 EMQX 集群（同 AZ），跨 AZ 由 EMQX Bridge 实现。

## TODO（后续）

1. EMQX → Kafka 直接 connector（替换 device-svc 桥接，提升吞吐）：评估 EMQX Kafka Hook Bridge。
2. device 凭证轮换：KMS 接入后实现 `device-svc/v1/devices/{id}:rotate-credentials`。
3. 设备影子 / 期望状态：当前只做事件管道；后续接入 EMQX retained message + device shadow 表。
4. 大规模 MQTT 压测：脚本与 `make bench-ws` 并列，目标 5w 同时在线 MQTT 客户端。
