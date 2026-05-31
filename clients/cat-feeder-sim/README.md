# cat-feeder-sim

模拟出粮机（猫舍喂食器）的 MQTT 客户端，用于：

- 本地一键起 N 个虚拟设备，订阅 `device/{id}/cmd/feed` / `cmd/cancel`，
  按概率回 `feed_ack` + `feed_done`；
- 周期发送 `heartbeat` 事件，触发 device-svc 刷新 `last_seen_at`；
- 联调 / 压测：测试 device-svc bridge、feeding-svc outbox relay、gateway 推送、
  LL-HLS 全链路在 100~10000 设备规模下的真实行为。

## 构建与运行

```bash
cd clients/cat-feeder-sim
go build -o cat-feeder-sim ./cmd/cat-feeder-sim

# 单台设备（dev_demo）：
./cat-feeder-sim --broker tcp://localhost:1883 --devices 1 --device-prefix dev_demo

# 1000 台并发设备，5% 失败率：
./cat-feeder-sim \
  --broker tcp://localhost:1883 \
  --devices 1000 \
  --device-prefix cat_sim \
  --room-prefix room_sim \
  --fail-rate 0.05 \
  --feed-latency-ms 100-800 \
  --heartbeat-secs 5
```

## CLI 参数

| 标志 | 默认 | 说明 |
| --- | --- | --- |
| `--broker` | `tcp://localhost:1883` | 逗号分隔多 broker 支持 |
| `--devices` | `1` | 并发模拟设备数 |
| `--device-prefix` | `sim_dev_` | device_id 前缀；`{prefix}{i:06d}` |
| `--room-prefix` | `room_sim_` | room_id 前缀，用于 ack payload 填充 |
| `--username` | `""` | MQTT 用户名（启用 EMQX ACL 时需传） |
| `--password` | `""` | MQTT 密码（建议向 device-svc /mqtt-credential 申请） |
| `--fail-rate` | `0.0` | 设备故意回 `failed` 的概率（0–1）|
| `--feed-latency-ms` | `100-800` | 出粮模拟延迟范围（min-max） |
| `--heartbeat-secs` | `5` | 心跳周期；0 表示禁用 |
| `--qos` | `1` | MQTT QoS（0/1/2） |
| `--clean-session` | `true` | MQTT clean session |
| `--prom-listen` | `:9301` | Prometheus `/metrics` 监听；空则不暴露 |
| `--connect-jitter-ms` | `200` | 启动时连接 jitter（防雪崩） |

## 主题约定（与 ADR-0012 一致）

- 上行（设备 → 平台）：
  - `device/{id}/event/heartbeat`：心跳，payload 含 `remaining_food_grams`
  - `device/{id}/event/feed_ack`：收到 cmd 立即回（状态 = `acked`）
  - `device/{id}/event/feed_done`：执行完出粮（状态 = `succeeded` / `failed`）
  - `device/{id}/event/error`：硬件异常等
- 下行（平台 → 设备）：
  - `device/{id}/cmd/feed`：投喂命令；payload 见 `bridge.FeedCommandPayload`
  - `device/{id}/cmd/cancel`：取消命令

## 指标

- `cat_feeder_sim_devices_total`：模拟设备总数
- `cat_feeder_sim_feed_commands_total{result=ack|done|failed}`：投喂事件计数
- `cat_feeder_sim_feed_latency_seconds`：cmd→done 端到端 latency histogram

## 与 e2e 链路联动

1. `make dev-up` 启起 EMQX + Postgres + Kafka；
2. 启动 device-svc / feeding-svc（带 `YUNMAO_MQTT_BROKERS` + `YUNMAO_EVENT_BUS=kafka`）；
3. 在另一个终端：`./cat-feeder-sim --devices 100`；
4. POST `/api/v1/feed-requests` 触发投喂，观察：
   - device-svc bridge 指标 `yunmao_device_bridge_cmd_out_total`
   - cat-feeder-sim 指标 `cat_feeder_sim_feed_commands_total`
   - feeding-svc 状态机推进至 `succeeded`
