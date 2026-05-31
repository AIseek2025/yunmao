// Package bridge 把 Kafka 与 MQTT 双向桥接：
//
//   - 下行：订阅 Kafka topic `feed.command.dispatched` / `feed.command.requested`，
//     按 device_id 路由到 MQTT `device/{device_id}/cmd/feed`；
//   - 上行：订阅 MQTT `device/+/event/+`，将设备事件翻译为 Kafka 事件
//     （`device.state.changed` / `feed.command.acked` / `feed.command.completed`）。
//
// 设计：
//
//   - 同进程内启动两条 goroutine 处理两个方向；ctx 取消后整体退出。
//   - 不阻塞 device-svc 的 HTTP 启动；连接失败时记录指标并重试（由 mqttx.AutoReconnect 兜底）。
//   - 命令 payload 用 yunmao.live/services/feeding-svc/publisher.FeedCommandRequested 结构，
//     与 outbox 写入的 CloudEvents payload 完全一致。
//
// 与 ADR-0012 对齐。
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"yunmao.live/pkg/yunmao/cloudevents"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/mqttx"
)

// FeedCommandPayload 是发往设备的下行 payload，与 publisher.FeedCommandRequested 字段对齐。
//
// 第四轮：增加 Reason 字段以支持 cancel 路径下游传递取消原因。
type FeedCommandPayload struct {
	FeedRequestID   string `json:"feed_request_id"`
	DeviceCommandID string `json:"device_command_id"`
	DeviceID        string `json:"device_id"`
	RoomID          string `json:"room_id"`
	AmountGrams     uint32 `json:"amount_grams"`
	MotorDurationMs uint32 `json:"motor_duration_ms"`
	ExpiresAt       string `json:"expires_at"`
	Reason          string `json:"reason,omitempty"`
}

// FeedAckPayload 是设备上行 ack，与 publisher.FeedCommandAcked 字段对齐。
type FeedAckPayload struct {
	FeedRequestID      string   `json:"feed_request_id"`
	DeviceCommandID    string   `json:"device_command_id"`
	DeviceID           string   `json:"device_id"`
	RoomID             string   `json:"room_id"`
	Status             string   `json:"status"`
	ActualAmountGrams  uint32   `json:"actual_amount_grams"`
	RemainingFoodGrams uint32   `json:"remaining_food_grams"`
	ExecutedAt         string   `json:"executed_at"`
	Errors             []string `json:"errors"`
}

// Config 桥接配置。
type Config struct {
	// Source 写入 CloudEvents source 字段。
	Source string
	// CommandTopic 内部命名空间；默认从 eventbus.TopicFeedCommandDispatched 派生。
	CommandSubscribeTopic eventbus.Topic
	// DeviceEventFilter 监听设备上行的 topic filter；默认 "device/+/event/+"。
	DeviceEventFilter string
	// KafkaGroup Kafka 消费组；不可与其它 service 复用。
	KafkaGroup string
}

// DefaultConfig 默认值。
func DefaultConfig() Config {
	return Config{
		Source:                "device-svc@bridge",
		CommandSubscribeTopic: eventbus.TopicFeedCommandDispatched,
		DeviceEventFilter:     "device/+/event/+",
		KafkaGroup:            "device-svc-bridge",
	}
}

// Bridge 是双向桥接器。
type Bridge struct {
	cfg  Config
	bus  eventbus.Bus
	mqtt mqttx.Client

	// onAck 注入回调，让 device-svc service 同步内存 device 状态（last_seen, remaining）。
	onAck func(ctx context.Context, ack FeedAckPayload)
	// onHeartbeat 设备心跳事件回调（heartbeat / online / telemetry）。
	onHeartbeat func(ctx context.Context, deviceID string, at time.Time)
}

// New 构造。
func New(bus eventbus.Bus, mqtt mqttx.Client, cfg Config) *Bridge {
	if cfg.Source == "" {
		cfg.Source = "device-svc@bridge"
	}
	if cfg.CommandSubscribeTopic == "" {
		cfg.CommandSubscribeTopic = eventbus.TopicFeedCommandDispatched
	}
	if cfg.DeviceEventFilter == "" {
		cfg.DeviceEventFilter = "device/+/event/+"
	}
	if cfg.KafkaGroup == "" {
		cfg.KafkaGroup = "device-svc-bridge"
	}
	return &Bridge{cfg: cfg, bus: bus, mqtt: mqtt}
}

// SetOnAck 注册 ack 回调（用于 device-svc 内部状态聚合）。
func (b *Bridge) SetOnAck(f func(ctx context.Context, ack FeedAckPayload)) {
	b.onAck = f
}

// SetOnHeartbeat 注册心跳回调；用于刷新 devices.last_seen_at。
func (b *Bridge) SetOnHeartbeat(f func(ctx context.Context, deviceID string, at time.Time)) {
	b.onHeartbeat = f
}

// Start 启动两个方向的桥接 goroutine。Stop 由 ctx 取消统一控制。
func (b *Bridge) Start(ctx context.Context) error {
	// downlink: Kafka -> MQTT
	if err := b.bus.Subscribe(ctx, b.cfg.KafkaGroup,
		[]eventbus.Topic{
			b.cfg.CommandSubscribeTopic,
			eventbus.TopicFeedCommandRequested,
			eventbus.TopicFeedCommandCancelled,
		},
		b.handleKafkaCmd); err != nil {
		return fmt.Errorf("bridge: kafka subscribe: %w", err)
	}
	// uplink: MQTT -> Kafka
	if err := b.mqtt.Subscribe(ctx, b.cfg.DeviceEventFilter, mqttx.QoS1, b.handleMqttEvent); err != nil {
		return fmt.Errorf("bridge: mqtt subscribe: %w", err)
	}
	return nil
}

func (b *Bridge) handleKafkaCmd(ctx context.Context, env eventbus.Envelope) error {
	bridgeCmdInTotal.WithLabelValues(env.Topic.String()).Inc()
	// 解开 CloudEvents.data
	var ce struct {
		Data FeedCommandPayload `json:"data"`
	}
	if err := json.Unmarshal(env.Payload, &ce); err != nil {
		bridgeCmdErrTotal.WithLabelValues("parse").Inc()
		return err
	}
	if ce.Data.DeviceID == "" {
		bridgeCmdErrTotal.WithLabelValues("no_device").Inc()
		return nil
	}
	// 按 topic 决定下行 MQTT cmd 子类型。
	mqttCmd := "feed"
	if env.Topic == eventbus.TopicFeedCommandCancelled {
		mqttCmd = "cancel"
	}
	topic := mqttx.DeviceCommandTopic(ce.Data.DeviceID, mqttCmd)
	payload, err := json.Marshal(ce.Data)
	if err != nil {
		return err
	}
	start := time.Now()
	if err := b.mqtt.Publish(ctx, topic, mqttx.QoS1, payload); err != nil {
		bridgeCmdErrTotal.WithLabelValues("publish").Inc()
		return err
	}
	bridgeCmdOutLatency.Observe(time.Since(start).Seconds())
	bridgeCmdOutTotal.WithLabelValues(ce.Data.DeviceID).Inc()
	return nil
}

func (b *Bridge) handleMqttEvent(ctx context.Context, m mqttx.Message) error {
	// topic = device/{device_id}/event/{event_type}
	parts := strings.Split(m.Topic, "/")
	if len(parts) < 4 || parts[0] != "device" || parts[2] != "event" {
		return nil
	}
	deviceID := parts[1]
	eventType := parts[3]
	bridgeEvtInTotal.WithLabelValues(eventType).Inc()

	switch eventType {
	case "feed_ack", "feed_done":
		var ack FeedAckPayload
		if err := json.Unmarshal(m.Payload, &ack); err != nil {
			bridgeEvtErrTotal.WithLabelValues("parse").Inc()
			return err
		}
		ack.DeviceID = deviceID
		// 1) 内部回调
		if b.onAck != nil {
			b.onAck(ctx, ack)
		}
		// 2) 上 Kafka
		topic := eventbus.TopicFeedCommandAcked
		if eventType == "feed_done" || ack.Status == "succeeded" {
			topic = eventbus.TopicFeedCommandCompleted
		}
		return b.publishCE(ctx, topic, ack.RoomID, ack.FeedRequestID, ack)
	case "cancelled", "cancel_ack":
		var ack FeedAckPayload
		if err := json.Unmarshal(m.Payload, &ack); err != nil {
			bridgeEvtErrTotal.WithLabelValues("parse").Inc()
			return err
		}
		ack.DeviceID = deviceID
		// 通知 feeding-svc / outbox 状态机：投喂被设备确认取消。
		return b.publishCE(ctx, eventbus.TopicFeedCommandCancelledAcked, ack.RoomID, ack.FeedRequestID, ack)
	case "online", "offline", "heartbeat", "telemetry", "error":
		if eventType == "heartbeat" || eventType == "online" || eventType == "telemetry" {
			if b.onHeartbeat != nil {
				b.onHeartbeat(ctx, deviceID, time.Now().UTC())
			}
		}
		payload := map[string]any{
			"device_id":  deviceID,
			"event_type": eventType,
			"raw":        json.RawMessage(m.Payload),
		}
		return b.publishCE(ctx, eventbus.TopicDeviceStateChanged, deviceID, deviceID, payload)
	}
	return nil
}

func (b *Bridge) publishCE(ctx context.Context, topic eventbus.Topic, key, subject string, data any) error {
	ce := cloudevents.New[any](string(topic), b.cfg.Source, subject, data)
	env, err := eventbus.NewEnvelope(topic, key, ce)
	if err != nil {
		return err
	}
	if err := b.bus.Publish(ctx, env); err != nil {
		bridgeEvtErrTotal.WithLabelValues("kafka_publish").Inc()
		return err
	}
	bridgeEvtOutTotal.WithLabelValues(topic.String()).Inc()
	return nil
}

// ---------------- metrics ----------------

var (
	bridgeCmdInTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_device_bridge_cmd_in_total",
		Help: "Commands received from Kafka by device-svc bridge.",
	}, []string{"topic"})

	bridgeCmdOutTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_device_bridge_cmd_out_total",
		Help: "Commands forwarded to MQTT by device-svc bridge.",
	}, []string{"device_id"})

	bridgeCmdOutLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "yunmao_device_bridge_cmd_out_latency_seconds",
		Help:    "Latency of bridge.publishCmd (Kafka receive -> MQTT publish).",
		Buckets: prometheus.DefBuckets,
	})

	bridgeCmdErrTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_device_bridge_cmd_err_total",
		Help: "Errors while forwarding commands.",
	}, []string{"reason"})

	bridgeEvtInTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_device_bridge_evt_in_total",
		Help: "Device events received via MQTT.",
	}, []string{"event_type"})

	bridgeEvtOutTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_device_bridge_evt_out_total",
		Help: "Device events published to Kafka.",
	}, []string{"topic"})

	bridgeEvtErrTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_device_bridge_evt_err_total",
		Help: "Errors while forwarding device events.",
	}, []string{"reason"})
)
