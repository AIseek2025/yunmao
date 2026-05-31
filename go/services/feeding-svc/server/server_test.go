// 工厂层 smoke test：用 in-memory cache + memory eventbus 起完整 server，HTTP 投喂走通。
package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/cache"
	"yunmao.live/pkg/yunmao/eventbus"

	"yunmao.live/services/feeding-svc/publisher"
	"yunmao.live/services/feeding-svc/server"
	"yunmao.live/services/feeding-svc/internal/service"
)

func TestServerFactory_CreateFeedRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := cache.Open(ctx, cache.Config{Backend: cache.BackendMemory})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	defer store.Close()

	bus := eventbus.NewMemoryBus()
	defer func() { _ = bus.Close() }()

	srv, err := server.New(ctx, server.Deps{
		Cache:     store,
		Publisher: publisher.NewKafka(bus, "feeding-svc@test"),
		Bus:       nil,
		Source:    "feeding-svc@test",
		Region:    "test",
		SeedRooms: []server.SeedRoom{{
			ID:          "room_test",
			CatID:       "cat_test",
			DeviceID:    "dev_test",
			FeedingOpen: true,
		}},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer srv.Close()

	hs := httptest.NewServer(srv.HTTP)
	defer hs.Close()

	body := map[string]any{
		"user_id":         "user_test_1",
		"room_id":         "room_test",
		"amount_grams":    8,
		"idempotency_key": "idem_test_1",
	}
	b, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, hs.URL+"/api/v1/feed-requests", bytes.NewReader(b))
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusAccepted)
	}
	var out service.Request
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ID == "" {
		t.Fatalf("expected feed_request_id, got empty: %#v", out)
	}
	if out.RoomID != "room_test" {
		t.Fatalf("room mismatch: %s", out.RoomID)
	}

	// 给 async publisher 一点点时间。
	time.Sleep(50 * time.Millisecond)
}
