// outbox_listener.go 把 service.StateChangeEvent 翻译为 store.Transition + outbox 行写入。
//
// 与 ADR-0010 对齐：
//
//   - 一次状态变更对应一次 store.SaveTransition 调用（单事务）；
//   - 当变更涉及 Kafka 发布时（如 accepted→queued 触发 feed.command.requested），同一事务内
//     插入 outbox 行；relay worker 异步推送到 Kafka。
package service

import (
	"context"

	"yunmao.live/pkg/yunmao/cloudevents"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/feedstate"

	"yunmao.live/services/feeding-svc/publisher"
	"yunmao.live/services/feeding-svc/internal/store"
)

// OutboxListener 是 service.EventListener 的实现：把状态变更落到 PG 并准备 outbox。
type OutboxListener struct {
	Store  store.Store
	Source string
}

// Handle 实现 EventListener。
func (l *OutboxListener) Handle(ctx context.Context, ev StateChangeEvent) {
	snap := toStoreRequest(ev.Snapshot)
	outboxEvents := buildOutboxEvents(l.Source, ev)

	t := store.Transition{
		Request:      snap,
		From:         ev.From,
		To:           ev.To,
		Reason:       ev.Reason,
		Actor:        l.Source,
		OutboxEvents: outboxEvents,
	}
	if ev.From == feedstate.Created {
		// 第一笔事件：行尚未存在
		if err := l.Store.Create(ctx, t); err != nil {
			outboxListenerErrTotal.WithLabelValues("create").Inc()
			return
		}
		outboxListenerOKTotal.WithLabelValues("create").Inc()
		return
	}
	if err := l.Store.SaveTransition(ctx, t); err != nil {
		outboxListenerErrTotal.WithLabelValues("save").Inc()
		return
	}
	outboxListenerOKTotal.WithLabelValues("save").Inc()
}

// buildOutboxEvents 根据当前迁移决定该写入哪些 outbox 行。
//
// 与 04-设备接入数据模型与API边界.md 10.2 节对齐：
//
//   - accepted → queued     : feed.command.requested（key=device_id；下行 device-svc）
//   - queued → dispatched   : feed.command.dispatched（key=room_id；扇出 gateway）
//   - dispatched → acknowledged : feed.command.acked（key=room_id）
//   - acknowledged → succeeded  : feed.command.completed（key=room_id）
//   - * → failed                : feed.command.failed（key=room_id）
func buildOutboxEvents(source string, ev StateChangeEvent) []store.OutboxEvent {
	r := ev.Snapshot
	switch {
	case ev.From == feedstate.Accepted && ev.To == feedstate.Queued:
		cmd := publisher.FeedCommandRequested{
			FeedRequestID:   r.ID,
			DeviceCommandID: r.DeviceCommandID,
			DeviceID:        r.DeviceID,
			RoomID:          r.RoomID,
			AmountGrams:     r.AmountGrams,
			MotorDurationMs: 1200,
			ExpiresAt:       r.UpdatedAt.Add(30 * 1_000_000_000).Format("2006-01-02T15:04:05Z07:00"),
		}
		return []store.OutboxEvent{
			ce(eventbus.TopicFeedCommandRequested, r.DeviceID, source, r.ID, cmd),
		}
	case ev.From == feedstate.Queued && ev.To == feedstate.Dispatched:
		payload := map[string]any{
			"feed_request_id":   r.ID,
			"device_command_id": r.DeviceCommandID,
			"device_id":         r.DeviceID,
			"room_id":           r.RoomID,
			"amount_grams":      r.AmountGrams,
		}
		return []store.OutboxEvent{
			ce(eventbus.TopicFeedCommandDispatched, r.RoomID, source, r.ID, payload),
		}
	case ev.From == feedstate.Dispatched && ev.To == feedstate.Acknowledged:
		payload := map[string]any{
			"feed_request_id":   r.ID,
			"device_command_id": r.DeviceCommandID,
			"room_id":           r.RoomID,
		}
		return []store.OutboxEvent{
			ce(eventbus.TopicFeedCommandAcked, r.RoomID, source, r.ID, payload),
		}
	case ev.From == feedstate.Acknowledged && ev.To == feedstate.Succeeded:
		payload := map[string]any{
			"feed_request_id":   r.ID,
			"device_command_id": r.DeviceCommandID,
			"room_id":           r.RoomID,
			"amount_grams":      r.AmountGrams,
		}
		return []store.OutboxEvent{
			ce(eventbus.TopicFeedCommandCompleted, r.RoomID, source, r.ID, payload),
		}
	case ev.To == feedstate.Failed:
		payload := map[string]any{
			"feed_request_id":   r.ID,
			"device_command_id": r.DeviceCommandID,
			"room_id":           r.RoomID,
			"device_id":         r.DeviceID,
			"reason":            ev.Reason,
		}
		return []store.OutboxEvent{
			ce(eventbus.TopicFeedCommandFailed, r.RoomID, source, r.ID, payload),
		}
	case ev.To == feedstate.Rejected && (ev.From == feedstate.Queued || ev.From == feedstate.Dispatched):
		// 取消路径：device-svc 收到后会向设备发 cancel 指令。
		payload := map[string]any{
			"feed_request_id":   r.ID,
			"device_command_id": r.DeviceCommandID,
			"device_id":         r.DeviceID,
			"room_id":           r.RoomID,
			"reason":            ev.Reason,
		}
		return []store.OutboxEvent{
			ce(eventbus.TopicFeedCommandCancelled, r.DeviceID, source, r.ID, payload),
		}
	}
	return nil
}

func ce(topic eventbus.Topic, partitionKey string, source, subject string, payload any) store.OutboxEvent {
	return store.OutboxEvent{
		Topic:        topic,
		PartitionKey: partitionKey,
		CloudEvent:   cloudevents.New[any](string(topic), source, subject, payload),
		Headers: map[string]string{
			"content-type": "application/cloudevents+json",
			"ce-type":      string(topic),
		},
	}
}

func toStoreRequest(r Request) store.FeedRequest {
	return store.FeedRequest{
		ID:              r.ID,
		UserID:          r.UserID,
		RoomID:          r.RoomID,
		CatID:           r.CatID,
		DeviceID:        r.DeviceID,
		DeviceCommandID: r.DeviceCommandID,
		AmountGrams:     r.AmountGrams,
		Status:          r.Status,
		IdempotencyKey:  r.IdempotencyKey,
		RejectReason:    r.RejectReason,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}
