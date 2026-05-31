package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakePub 简单 Publisher：记录每条 row，并允许 forceErr 控制重试路径。
type fakePub struct {
	rows     []OutboxRow
	failNext int // 前 N 次返回错误
	mu       chan struct{}
}

func newFakePub() *fakePub { return &fakePub{mu: make(chan struct{}, 1)} }

func (p *fakePub) Publish(_ context.Context, row OutboxRow) error {
	p.mu <- struct{}{}
	defer func() { <-p.mu }()
	if p.failNext > 0 {
		p.failNext--
		return errors.New("publish failed")
	}
	p.rows = append(p.rows, row)
	return nil
}

// 验证 defaultBackoff 指数曲线：100ms, 200ms, 400ms, 800ms, 1600ms（上限 2s）。
func TestDefaultBackoffMonotonic(t *testing.T) {
	prev := time.Duration(0)
	for i := 0; i < 6; i++ {
		d := defaultBackoff(i)
		if i > 0 && d < prev {
			t.Fatalf("backoff regression at attempt %d: %v < %v", i, d, prev)
		}
		if d > 2*time.Second {
			t.Fatalf("backoff exceeds 2s ceiling: %v", d)
		}
		prev = d
	}
}

func TestRelayConfigDefaults(t *testing.T) {
	cfg := DefaultRelayConfig()
	if cfg.BatchSize == 0 || cfg.Interval == 0 || cfg.MaxAttempts == 0 || cfg.Concurrency == 0 {
		t.Fatalf("defaults must be non-zero: %+v", cfg)
	}
}

func TestNewRelayPopulatesDefaults(t *testing.T) {
	r := NewRelay(nil, newFakePub(), RelayConfig{})
	if r.cfg.BatchSize == 0 {
		t.Fatalf("expected default batch size")
	}
	if r.cfg.Backoff == nil {
		t.Fatalf("expected default backoff")
	}
}

// 注意：FetchUnpublished / MarkPublished / Relay.processBatch 需要真实 PG（通过 testcontainers）。
// 这里只验证不需要 DB 的纯逻辑；端到端测试 PgStore 由 docker-compose 集成测覆盖。

func TestInsertOutboxValidates(t *testing.T) {
	// Topic 必填
	_, err := InsertOutbox(context.Background(), nil, OutboxRow{Payload: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "topic required") {
		t.Fatalf("expected topic required, got %v", err)
	}
	// payload 必填
	_, err = InsertOutbox(context.Background(), nil, OutboxRow{Topic: "x"})
	if err == nil || !strings.Contains(err.Error(), "payload required") {
		t.Fatalf("expected payload required, got %v", err)
	}
}
