package bridge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/cloudevents"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/mqttx"
)

// 这个测试构建：
//
//	feeding-svc 模拟 → Kafka memorybus → bridge.handleKafkaCmd → MQTT → 设备模拟 → 上行 → bridge.handleMqttEvent → Kafka
//
// 全部走 memory 实现，无需真实 broker。
func TestBridgeEndToEnd_KafkaMqttKafka(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })

	mqtt := mqttx.NewMemoryBroker().NewClient("device-svc")
	if err := mqtt.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mqtt.Disconnect(0) })

	// 模拟“设备”：另一个 MQTT 客户端，订阅命令并发布 ack。
	device := mqttx.NewMemoryBroker()
	// 共享同一个 broker：需要把 device 客户端注册到同一 broker
	// 这里简化：device-svc 与 device 都用同一个 broker。
	deviceClient := newSharedClient(mqtt)
	_ = deviceClient // 不直接使用；因为 memoryBroker 内部按 clientID 聚合，订阅会扇出
	_ = device

	bridge := New(bus, mqtt, DefaultConfig())

	// 收集 Kafka 出口
	var kafkaMu sync.Mutex
	var kafkaEvents []eventbus.Envelope
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动一个独立消费者监听 device.state.changed + feed.command.*
	if err := bus.Subscribe(ctx, "test-collector", []eventbus.Topic{
		eventbus.TopicDeviceStateChanged,
		eventbus.TopicFeedCommandAcked,
		eventbus.TopicFeedCommandCompleted,
	}, func(_ context.Context, env eventbus.Envelope) error {
		kafkaMu.Lock()
		defer kafkaMu.Unlock()
		kafkaEvents = append(kafkaEvents, env)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 模拟设备：订阅命令 topic，立即回复 ack
	var deviceCmdReceived int32
	if err := mqtt.Subscribe(ctx, "device/+/cmd/feed", mqttx.QoS1, func(_ context.Context, m mqttx.Message) error {
		deviceCmdReceived++
		// 取 device id
		parts := []byte(m.Topic)
		_ = parts
		// 立刻回 ack
		ack := FeedAckPayload{
			FeedRequestID:     "feed_1",
			DeviceCommandID:   "cmd_1",
			DeviceID:          "dev_demo",
			RoomID:            "room_demo",
			Status:            "succeeded",
			ActualAmountGrams: 5,
			ExecutedAt:        time.Now().UTC().Format(time.RFC3339),
		}
		body, _ := json.Marshal(ack)
		return mqtt.Publish(context.Background(), mqttx.DeviceEventTopic("dev_demo", "feed_done"), mqttx.QoS1, body)
	}); err != nil {
		t.Fatal(err)
	}

	// 启动 bridge（在已经订阅 mqtt 之后）
	if err := bridge.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// 发一个 feed.command.dispatched 模拟 outbox relay
	payload := FeedCommandPayload{
		FeedRequestID:   "feed_1",
		DeviceCommandID: "cmd_1",
		DeviceID:        "dev_demo",
		RoomID:          "room_demo",
		AmountGrams:     5,
		MotorDurationMs: 1200,
	}
	ce := cloudevents.New[any](string(eventbus.TopicFeedCommandDispatched), "test", "feed_1", payload)
	env, _ := eventbus.NewEnvelope(eventbus.TopicFeedCommandDispatched, "room_demo", ce)
	if err := bus.Publish(ctx, env); err != nil {
		t.Fatal(err)
	}

	// 等待端到端
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		kafkaMu.Lock()
		got := len(kafkaEvents)
		kafkaMu.Unlock()
		if got >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if deviceCmdReceived == 0 {
		t.Fatal("device did not receive MQTT command")
	}
	kafkaMu.Lock()
	defer kafkaMu.Unlock()
	if len(kafkaEvents) == 0 {
		t.Fatal("kafka did not see device ack")
	}
	if string(kafkaEvents[0].Topic) != string(eventbus.TopicFeedCommandCompleted) {
		t.Fatalf("expected feed.command.completed first, got %s", kafkaEvents[0].Topic)
	}
}

// newSharedClient 仅供文档；真实 MemoryBroker 多客户端模式见 TestMemoryBrokerPubSub。
func newSharedClient(c mqttx.Client) mqttx.Client { return c }
