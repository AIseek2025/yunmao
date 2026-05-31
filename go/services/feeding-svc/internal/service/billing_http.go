// billing_http.go：BillingHook 的 HTTP 适配；调用 billing-svc 的 wallet saga 端点。
//
// 设计：
//   - Reserve → POST /api/v1/wallets/holds，返回 hold.id 作为 receipt_id；
//   - Confirm → POST /api/v1/wallets/holds/{id}/confirm；
//   - Cancel  → POST /api/v1/wallets/holds/{id}/cancel；
//   - 5s 超时；任何 4xx/5xx 都转 error；调用方根据 feature flag 决定降级。
//
// feature flag `billing.required=true` 时 Reserve 失败短路；`=false` 时 Reserve 失败
// 仍允许投喂（fallback NoopBilling）。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPBilling BillingHook 的 HTTP 适配；针对 billing-svc。
type HTTPBilling struct {
	// BaseURL 必填，例如 "http://billing-svc:8202"。
	BaseURL string
	// HTTPClient 可选；nil 时使用 5s 超时的默认。
	HTTPClient *http.Client
	// FallbackOnError true 时 Reserve 失败回 (noop, nil)；用于免费阶段降级。
	FallbackOnError bool
}

// NewHTTPBilling 构造。
func NewHTTPBilling(baseURL string) *HTTPBilling {
	return &HTTPBilling{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type httpReserveReq struct {
	UserID         string `json:"user_id"`
	RoomID         string `json:"room_id"`
	CatID          string `json:"cat_id"`
	FeedRequestID  string `json:"feed_request_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	AmountGrams    uint32 `json:"amount_grams"`
	TTLSeconds     int    `json:"ttl_seconds,omitempty"`
}

type httpHoldResp struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Reserve 调用 billing-svc /api/v1/wallets/holds。
func (h *HTTPBilling) Reserve(ctx context.Context, in BillingReserveInput) (string, error) {
	req := httpReserveReq{
		UserID:         in.UserID,
		RoomID:         in.RoomID,
		CatID:          in.CatID,
		IdempotencyKey: in.IdempotencyKey,
		AmountGrams:    in.AmountGrams,
		TTLSeconds:     60,
	}
	resp, err := h.post(ctx, "/api/v1/wallets/holds", req)
	if err != nil {
		if h.FallbackOnError {
			return "noop", nil
		}
		return "", err
	}
	return resp.ID, nil
}

// Confirm 调用 billing-svc /api/v1/wallets/holds/{id}/confirm。
func (h *HTTPBilling) Confirm(ctx context.Context, receiptID string) error {
	if receiptID == "noop" {
		return nil
	}
	_, err := h.post(ctx, "/api/v1/wallets/holds/"+receiptID+"/confirm", nil)
	return err
}

// Cancel 调用 billing-svc /api/v1/wallets/holds/{id}/cancel。
func (h *HTTPBilling) Cancel(ctx context.Context, receiptID string) error {
	if receiptID == "noop" {
		return nil
	}
	_, err := h.post(ctx, "/api/v1/wallets/holds/"+receiptID+"/cancel", map[string]string{"reason": "feeding-svc cancel"})
	return err
}

func (h *HTTPBilling) post(ctx context.Context, path string, body any) (*httpHoldResp, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	cli := h.HTTPClient
	if cli == nil {
		cli = &http.Client{Timeout: 5 * time.Second}
	}
	r, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode >= 400 {
		b, _ := io.ReadAll(r.Body)
		return nil, fmt.Errorf("billing-svc %s -> %d: %s", path, r.StatusCode, string(b))
	}
	if r.StatusCode == http.StatusNoContent {
		return &httpHoldResp{}, nil
	}
	var out httpHoldResp
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		if errors.Is(err, io.EOF) {
			return &out, nil
		}
		return nil, err
	}
	return &out, nil
}
