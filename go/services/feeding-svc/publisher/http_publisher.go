package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HTTPPublisher 是直接 HTTP 调用 device-edge / gateway 的 publisher（旧 PoC 路径）。
type HTTPPublisher struct {
	deviceEdgeURL string
	gatewayURL    string
	client        *http.Client
}

// NewHTTP 构造 HTTP publisher。
func NewHTTP(deviceEdge, gateway string) *HTTPPublisher {
	return &HTTPPublisher{
		deviceEdgeURL: deviceEdge,
		gatewayURL:    gateway,
		client:        &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *HTTPPublisher) PublishFeedCommandRequested(ctx context.Context, evt FeedCommandRequested) error {
	if p.deviceEdgeURL == "" {
		return nil
	}
	return p.post(ctx, p.deviceEdgeURL+"/commands", evt)
}

func (p *HTTPPublisher) PublishGatewayEvent(ctx context.Context, eventType, roomID string, data any) error {
	if p.gatewayURL == "" {
		return nil
	}
	body := map[string]any{
		"room_id":    roomID,
		"event_type": eventType,
		"data":       data,
	}
	return p.post(ctx, p.gatewayURL+"/publish", body)
}

func (p *HTTPPublisher) post(ctx context.Context, url string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("publish to %s: status %d", url, resp.StatusCode)
	}
	return nil
}
