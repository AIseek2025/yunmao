// Package cloudevents 与 yunmao-common::cloudevents 对齐的 CloudEvents 1.0 信封。
package cloudevents

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event CloudEvents 1.0 信封。
type Event[T any] struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	Type            string    `json:"type"`
	Subject         string    `json:"subject,omitempty"`
	Time            time.Time `json:"time"`
	SpecVersion     string    `json:"specversion"`
	DataSchema      string    `json:"dataschema,omitempty"`
	DataContentType string    `json:"datacontenttype,omitempty"`
	Data            T         `json:"data"`
}

// New 构造一个 Event；ID 自动 UUIDv4，Time 取 UTC now。
func New[T any](eventType, source, subject string, data T) Event[T] {
	return Event[T]{
		ID:              uuid.NewString(),
		Source:          source,
		Type:            eventType,
		Subject:         subject,
		Time:            time.Now().UTC(),
		SpecVersion:     "1.0",
		DataContentType: "application/json",
		Data:            data,
	}
}

// MarshalJSON 序列化为 CloudEvents JSON。
func (e Event[T]) MarshalJSON() ([]byte, error) {
	type alias Event[T]
	return json.Marshal(alias(e))
}
