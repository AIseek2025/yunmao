// Package publisher 给 feeding-svc 提供事件发布抽象与多后端实现。
//
// 后端 (`EVENT_BUS` 环境变量决定)：
//
//   - `memory`：进程内（PoC / 单测）。
//   - `http`：保留旧 PoC 直连：feeding-svc → device-edge `/commands`、gateway `/publish`。
//   - `kafka`：CI 默认。device-edge / gateway 都从 Kafka 消费，与 Rust 端互通。
package publisher

import (
	"context"
	"errors"
)

// FeedCommandRequested 出向事件。
type FeedCommandRequested struct {
	FeedRequestID   string `json:"feed_request_id"`
	DeviceCommandID string `json:"device_command_id"`
	DeviceID        string `json:"device_id"`
	RoomID          string `json:"room_id"`
	AmountGrams     uint32 `json:"amount_grams"`
	MotorDurationMs uint32 `json:"motor_duration_ms"`
	ExpiresAt       string `json:"expires_at"`
}

// FeedCommandAcked 反向事件（device-edge → feeding-svc，但也可能从 Kafka 消费回来）。
type FeedCommandAcked struct {
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

// EventPublisher 是发布抽象。
type EventPublisher interface {
	PublishFeedCommandRequested(ctx context.Context, evt FeedCommandRequested) error
	PublishGatewayEvent(ctx context.Context, eventType, roomID string, data any) error
}

// ErrUnknownPublisher 未知 publisher 类型。
var ErrUnknownPublisher = errors.New("publisher: unknown kind")
