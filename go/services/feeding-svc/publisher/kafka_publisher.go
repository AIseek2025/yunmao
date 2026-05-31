package publisher

import (
	"context"
	"fmt"

	"yunmao.live/pkg/yunmao/cloudevents"
	"yunmao.live/pkg/yunmao/eventbus"
)

// KafkaPublisher 通过 yunmao 事件总线发布事件；与 Rust 端 yunmao-eventbus 互通。
type KafkaPublisher struct {
	bus    eventbus.Bus
	source string
}

// NewKafka 构造 Kafka publisher。
func NewKafka(bus eventbus.Bus, source string) *KafkaPublisher {
	return &KafkaPublisher{bus: bus, source: source}
}

// PublishFeedCommandRequested 将事件包装为 CloudEvent，按 device_id partition。
func (p *KafkaPublisher) PublishFeedCommandRequested(ctx context.Context, evt FeedCommandRequested) error {
	ce := cloudevents.New[any](string(eventbus.TopicFeedCommandRequested), p.source, evt.FeedRequestID, evt)
	env, err := eventbus.NewEnvelope(eventbus.TopicFeedCommandRequested, evt.DeviceID, ce)
	if err != nil {
		return fmt.Errorf("kafka publish requested: %w", err)
	}
	return p.bus.Publish(ctx, env)
}

// PublishGatewayEvent 把房间扇出事件发布到 `feed.command.*`（或 `live.stream.*`）topic。
//
// 这里没有把 gateway 当作单独的服务，而是让 gateway 直接 subscribe Kafka 拿到事件再扇出，
// 与 HTTP /publish 路径在功能上等价但语义更清晰。
func (p *KafkaPublisher) PublishGatewayEvent(ctx context.Context, eventType, roomID string, data any) error {
	t := eventbus.Topic(eventType)
	ce := cloudevents.New[any](eventType, p.source, roomID, data)
	env, err := eventbus.NewEnvelope(t, roomID, ce)
	if err != nil {
		return fmt.Errorf("kafka publish gateway: %w", err)
	}
	return p.bus.Publish(ctx, env)
}
