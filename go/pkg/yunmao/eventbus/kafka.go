// KafkaBus 是 segmentio/kafka-go 实现。
//
// 选型说明：
//
//   - `kafka-go` 纯 Go，无 cgo / librdkafka 依赖，docker 镜像 < 20MB；
//     比 `confluent-kafka-go` 部署摩擦更小。
//   - 写入按 `Hash` partitioner，使用 Envelope.Key 做 partition；
//     `RequiredAcks=All` + `Async=false` 提供 at-least-once。
//   - 消费按 `GroupID` 加入消费组，逐条 commit，失败按 [`retryPolicy`] 重试，
//     最终走 DLQ topic（带 `.dlq` 后缀，分区 key 沿用原 key 便于 reprocess）。
package eventbus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// retryPolicy 控制单条消息重试上限。
type retryPolicy struct {
	MaxAttempts int
	BaseBackoff time.Duration
}

var defaultRetry = retryPolicy{MaxAttempts: 3, BaseBackoff: 250 * time.Millisecond}

// KafkaBus 是分布式事件总线。
type KafkaBus struct {
	cfg     Config
	writer  *kafka.Writer
	dlq     *kafka.Writer
	readers []*kafka.Reader
	mu      sync.Mutex
	closed  bool
}

// NewKafkaBus 构造 Kafka 后端；不立刻建连，按需创建。
func NewKafkaBus(cfg Config) *KafkaBus {
	w := &kafka.Writer{
		Addr:                   kafka.TCP(cfg.Brokers...),
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.Hash{},
		BatchTimeout:           50 * time.Millisecond,
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		Compression:            kafka.Snappy,
	}
	dlq := &kafka.Writer{
		Addr:                   kafka.TCP(cfg.Brokers...),
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.Hash{},
		BatchTimeout:           50 * time.Millisecond,
		RequiredAcks:           kafka.RequireAll,
	}
	return &KafkaBus{cfg: cfg, writer: w, dlq: dlq}
}

// Publish 写一条消息；topic 由 envelope 自带，使用 envelope.Key 作为 partition key。
func (k *KafkaBus) Publish(ctx context.Context, env Envelope) error {
	k.mu.Lock()
	closed := k.closed
	k.mu.Unlock()
	if closed {
		publishErrTotal.WithLabelValues(env.Topic.String()).Inc()
		return fmt.Errorf("eventbus(kafka): closed")
	}

	msg := kafka.Message{
		Topic: env.Topic.String(),
		Key:   []byte(env.Key),
		Value: env.Payload,
	}
	for hk, hv := range env.Headers {
		msg.Headers = append(msg.Headers, kafka.Header{Key: hk, Value: []byte(hv)})
	}
	if err := k.writer.WriteMessages(ctx, msg); err != nil {
		publishErrTotal.WithLabelValues(env.Topic.String()).Inc()
		return err
	}
	publishTotal.WithLabelValues(env.Topic.String()).Inc()
	return nil
}

// Subscribe 起一个 kafka reader 协程；按 ctx 关闭。
// 同一 group 内同 topic 的分区在多实例间被自动分配，给我们 at-least-once 语义。
func (k *KafkaBus) Subscribe(ctx context.Context, group string, topics []Topic, handler Handler) error {
	if group == "" {
		return fmt.Errorf("eventbus(kafka): subscribe requires group id")
	}
	if len(topics) == 0 {
		return fmt.Errorf("eventbus(kafka): subscribe requires at least one topic")
	}
	stringTopics := make([]string, len(topics))
	for i, t := range topics {
		stringTopics[i] = t.String()
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        k.cfg.Brokers,
		GroupID:        group,
		GroupTopics:    stringTopics,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 0, // 手动 commit
		StartOffset:    kafka.LastOffset,
	})
	k.mu.Lock()
	k.readers = append(k.readers, r)
	k.mu.Unlock()

	go func() {
		defer r.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			m, err := r.FetchMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				time.Sleep(defaultRetry.BaseBackoff)
				continue
			}
			env := messageToEnvelope(m)
			topicLabel := env.Topic.String()
			if hErr := dispatch(ctx, handler, env, defaultRetry); hErr != nil {
				dispatchErrTotal.WithLabelValues(topicLabel).Inc()
				_ = k.sendDLQ(ctx, env, hErr)
			} else {
				dispatchTotal.WithLabelValues(topicLabel).Inc()
			}
			if err := r.CommitMessages(ctx, m); err != nil && ctx.Err() == nil {
				// 提交失败下次还会拿到同一条；属于 at-least-once 的预期开销。
				time.Sleep(defaultRetry.BaseBackoff)
			}
		}
	}()
	return nil
}

func messageToEnvelope(m kafka.Message) Envelope {
	hdrs := make(map[string]string, len(m.Headers))
	for _, h := range m.Headers {
		hdrs[h.Key] = string(h.Value)
	}
	return Envelope{
		Topic:   Topic(m.Topic),
		Key:     string(m.Key),
		Headers: hdrs,
		Payload: m.Value,
	}
}

func dispatch(ctx context.Context, handler Handler, env Envelope, rp retryPolicy) error {
	var lastErr error
	for attempt := 1; attempt <= rp.MaxAttempts; attempt++ {
		err := handler(ctx, env)
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(rp.BaseBackoff * time.Duration(attempt)):
		}
	}
	return lastErr
}

func (k *KafkaBus) sendDLQ(ctx context.Context, env Envelope, cause error) error {
	dlqMsg := kafka.Message{
		Topic: env.Topic.DLQ().String(),
		Key:   []byte(env.Key),
		Value: env.Payload,
		Headers: append([]kafka.Header{
			{Key: "ce-dlq-reason", Value: []byte(cause.Error())},
		}, headerMap(env.Headers)...),
	}
	return k.dlq.WriteMessages(ctx, dlqMsg)
}

func headerMap(h map[string]string) []kafka.Header {
	out := make([]kafka.Header, 0, len(h))
	for k, v := range h {
		out = append(out, kafka.Header{Key: k, Value: []byte(v)})
	}
	return out
}

// Close 关闭 writer 与全部 reader。
func (k *KafkaBus) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	for _, r := range k.readers {
		_ = r.Close()
	}
	_ = k.writer.Close()
	_ = k.dlq.Close()
	return nil
}
