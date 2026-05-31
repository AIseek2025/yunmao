// Package eventbus 抽象 yunmao 事件总线（CloudEvents JSON 信封 + at-least-once）。
//
// 目标：
//
//   - 控制面（feeding-svc / device-svc / billing-svc / admin-svc）通过统一接口
//     发布 / 订阅 `feed.*`、`device.*`、`live.stream.*` 等领域事件。
//   - 单一 envelope 与 Rust 端 `yunmao-eventbus` 互通（CloudEvents 1.0 JSON）。
//   - 支持 `YUNMAO_EVENT_BUS=memory|kafka` 切换；CI 默认 kafka，单测使用 memory。
//
// 当前实现：
//
//   - `MemoryBus`：进程内通道，仅供测试 / 演示。
//   - `KafkaBus`：基于 `segmentio/kafka-go`，原因：纯 Go、无 librdkafka 依赖、镜像更小，
//     与多 Go service 部署模型契合。
//
// 关键约定：
//
//   - Topic 名约束：`<domain>.<entity>.<verb>`，例如 `feed.command.requested`，
//     由 [`TopicFeedCommandRequested`] 等常量收口；新事件必须先加常量再使用。
//   - 分区 key：按 `device_id`（指令）/`room_id`（房间扇出）选取，保证按设备 / 按房间有序。
//   - DLQ：每个消费者带 `<topic>.dlq` 后缀；超过最大重试次数后写入 DLQ 并返回 nil。
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"yunmao.live/pkg/yunmao/cloudevents"
)

// Topic 是事件总线 Topic 的强类型名，所有发布 / 订阅都用本类型。
type Topic string

// 与 04-设备接入数据模型与API边界.md 10.2 节对齐。新事件需在此追加常量。
const (
	TopicFeedRequestCreated     Topic = "feed.request.created"
	TopicFeedCommandRequested   Topic = "feed.command.requested"
	TopicFeedCommandDispatched  Topic = "feed.command.dispatched"
	TopicFeedCommandAcked       Topic = "feed.command.acked"
	TopicFeedCommandCompleted   Topic = "feed.command.completed"
	TopicFeedCommandFailed      Topic = "feed.command.failed"
	TopicFeedCommandCancelled       Topic = "feed.command.cancelled"
	TopicFeedCommandCancelledAcked  Topic = "feed.command.cancelled_acked"
	TopicDeviceStateChanged         Topic = "device.state.changed"
	TopicDeviceCommandRejected  Topic = "device.command.rejected"
	TopicLiveStreamOnline       Topic = "live.stream.online"
	TopicLiveStreamOffline      Topic = "live.stream.offline"
	TopicLiveStreamQualityDrop  Topic = "live.stream.quality_drop"

	// 第三轮新增：billing 订单事件（骨架；MVP 不接真实支付）。
	TopicOrderCreated  Topic = "order.created"
	TopicOrderPaid     Topic = "order.paid"
	TopicOrderRefunded Topic = "order.refunded"

	// 第六轮新增：billing 钱包冻结 / 确认 / 取消（saga）。
	TopicWalletReserved  Topic = "wallet.reserved"
	TopicWalletConfirmed Topic = "wallet.confirmed"
	TopicWalletCancelled Topic = "wallet.cancelled"

	// 第六轮新增：弹幕 / 审核。
	TopicChatMessage    Topic = "room.chat.message"
	TopicChatModeration Topic = "room.chat.moderation"
)

// DLQSuffix 死信 topic 后缀；调用方持有 base topic 即可派生。
const DLQSuffix = ".dlq"

// DLQ 返回 base topic 对应的 DLQ topic。
func (t Topic) DLQ() Topic { return t + DLQSuffix }

func (t Topic) String() string { return string(t) }

// Envelope 是发布与消费时的统一信封；`Data` 已是 CloudEvents JSON 字节序列（含 envelope）。
type Envelope struct {
	Topic Topic
	// Key 用于 Kafka partition；建议房间事件用 room_id，设备事件用 device_id。
	Key string
	// Headers 透传到 Kafka headers（content-type、ce-type 等）。
	Headers map[string]string
	// CloudEvent JSON 字节（即 `cloudevents.Event[any]` 的 Marshal 结果）。
	Payload []byte
}

// NewEnvelope 把 CloudEvents 信封打包成 Envelope。
func NewEnvelope(topic Topic, key string, evt cloudevents.Event[any]) (Envelope, error) {
	payload, err := evt.MarshalJSON()
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal cloudevent: %w", err)
	}
	return Envelope{
		Topic:   topic,
		Key:     key,
		Headers: map[string]string{"content-type": "application/cloudevents+json", "ce-type": string(topic)},
		Payload: payload,
	}, nil
}

// Bus 是统一事件总线抽象。Publish 至少一次，Subscribe 按 group 加入消费组。
type Bus interface {
	Publish(ctx context.Context, env Envelope) error
	Subscribe(ctx context.Context, group string, topics []Topic, handler Handler) error
	Close() error
}

// Handler 是消费者回调；返回 error 触发重试（受 RetryPolicy 控制），最终落 DLQ。
type Handler func(ctx context.Context, env Envelope) error

// ErrUnknownBackend 未识别的后端配置。
var ErrUnknownBackend = errors.New("eventbus: unknown backend")

// Backend 是后端类型。
type Backend string

const (
	BackendMemory Backend = "memory"
	BackendKafka  Backend = "kafka"
)

// Config 通用构造参数。
type Config struct {
	Backend Backend
	// Brokers 仅 KafkaBus 使用，逗号分隔。
	Brokers []string
	// ClientID 标识发布端 / 消费端。
	ClientID string
}

// Open 是工厂入口；按 `cfg.Backend` 返回相应实现。
func Open(cfg Config) (Bus, error) {
	switch cfg.Backend {
	case BackendMemory, "":
		return NewMemoryBus(), nil
	case BackendKafka:
		if len(cfg.Brokers) == 0 {
			return nil, fmt.Errorf("eventbus: kafka requires Brokers")
		}
		return NewKafkaBus(cfg), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownBackend, cfg.Backend)
	}
}

// --------------------------------------------------------------------------------------
// MemoryBus —— 单进程纯内存实现，仅用于单测 / `EVENT_BUS=memory` 的 PoC 模式。
// --------------------------------------------------------------------------------------

// MemoryBus 进程内事件总线。
type MemoryBus struct {
	mu      sync.Mutex
	subs    map[Topic][]chan Envelope
	closed  bool
	closeCh chan struct{}
}

// NewMemoryBus 构造内存事件总线。
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subs:    make(map[Topic][]chan Envelope),
		closeCh: make(chan struct{}),
	}
}

// Publish 同步把消息分发给当前订阅者；订阅者通道满则丢弃（用于内存场景避免阻塞）。
func (m *MemoryBus) Publish(_ context.Context, env Envelope) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		publishErrTotal.WithLabelValues(env.Topic.String()).Inc()
		return errors.New("eventbus: closed")
	}
	chans := append([]chan Envelope(nil), m.subs[env.Topic]...)
	m.mu.Unlock()
	for _, c := range chans {
		select {
		case c <- env:
		default:
			// MemoryBus 不保证 backpressure；满则丢，方便单测断言。
		}
	}
	publishTotal.WithLabelValues(env.Topic.String()).Inc()
	return nil
}

// Subscribe 注册订阅者；ctx 取消则解除订阅。
func (m *MemoryBus) Subscribe(ctx context.Context, _ string, topics []Topic, handler Handler) error {
	ch := make(chan Envelope, 256)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("eventbus: closed")
	}
	for _, t := range topics {
		m.subs[t] = append(m.subs[t], ch)
	}
	m.mu.Unlock()

	go func() {
		defer m.unsub(topics, ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.closeCh:
				return
			case env := <-ch:
				_ = handler(ctx, env) // memory 模式不做 DLQ；保持简洁
			}
		}
	}()
	return nil
}

func (m *MemoryBus) unsub(topics []Topic, ch chan Envelope) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range topics {
		s := m.subs[t]
		for i, c := range s {
			if c == ch {
				m.subs[t] = append(s[:i], s[i+1:]...)
				break
			}
		}
	}
}

// Close 关闭总线；后续 Publish 报错。
func (m *MemoryBus) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.closeCh)
	}
	return nil
}
