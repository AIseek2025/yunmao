// Package db: outbox.go 实现“事务性 outbox + relay worker”。
//
// 背景：
//
//   - 业务事务（如 feeding-svc 写 feed_requests + feeding_request_events）必须与 Kafka
//     消息的“逻辑发布”在同一原子单位里完成；不能依赖 Kafka 的事务（跨 PG / Kafka）。
//   - 解决方案：在业务事务里同时插入一行 `outbox`；relay worker 异步把未发布行投递到
//     Kafka，成功后写回 `published_at`。
//
// 本文件提供：
//
//   - `OutboxRow`：行结构。
//   - `InsertOutbox`：在外部已开启的 `pgx.Tx` 上插入一行（业务侧调用）。
//   - `Relay`：relay worker（独立 goroutine + `FOR UPDATE SKIP LOCKED` 抢占）。
//
// 与 ADR-0010 对齐。
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// OutboxRow 是 outbox 表的一行；与迁移 0002 中字段对齐。
type OutboxRow struct {
	ID           int64
	Topic        string
	PartitionKey string
	Payload      []byte
	Headers      map[string]string
	RegionID     string
	CreatedAt    time.Time
}

// InsertOutbox 在事务里插入一行。
func InsertOutbox(ctx context.Context, tx pgx.Tx, row OutboxRow) (int64, error) {
	if row.Topic == "" {
		return 0, errors.New("outbox: topic required")
	}
	if len(row.Payload) == 0 {
		return 0, errors.New("outbox: payload required")
	}
	region := row.RegionID
	if region == "" {
		region = "global"
	}
	headers := row.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	hb, err := json.Marshal(headers)
	if err != nil {
		return 0, fmt.Errorf("outbox: marshal headers: %w", err)
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO outbox (topic, partition_key, payload, headers, region_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, row.Topic, row.PartitionKey, row.Payload, hb, region).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("outbox: insert: %w", err)
	}
	return id, nil
}

// FetchUnpublished 抢占一批未发布行（FOR UPDATE SKIP LOCKED）。
//
// 调用者必须在同一事务里 [`MarkPublished`] / 提交。返回 nil 行表示无任务。
func FetchUnpublished(ctx context.Context, tx pgx.Tx, batch int) ([]OutboxRow, error) {
	if batch <= 0 {
		batch = 64
	}
	rows, err := tx.Query(ctx, `
		SELECT id, topic, partition_key, payload, COALESCE(headers, '{}'::jsonb), region_id, created_at
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, batch)
	if err != nil {
		return nil, fmt.Errorf("outbox: fetch: %w", err)
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		var r OutboxRow
		var hb []byte
		if err := rows.Scan(&r.ID, &r.Topic, &r.PartitionKey, &r.Payload, &hb, &r.RegionID, &r.CreatedAt); err != nil {
			return nil, err
		}
		if len(hb) > 0 {
			h := map[string]string{}
			if err := json.Unmarshal(hb, &h); err != nil {
				return nil, err
			}
			r.Headers = h
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkPublished 在同一事务里把 ID 标记为已发布。
func MarkPublished(ctx context.Context, tx pgx.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE outbox SET published_at = NOW() WHERE id = ANY($1)
	`, ids)
	return err
}

// MarkDLQ 把行从 outbox 删除并写入 outbox_dlq；失败重试上限后调用。
func MarkDLQ(ctx context.Context, tx pgx.Tx, row OutboxRow, reason string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_dlq (id, topic, partition_key, payload, headers, region_id, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING
	`, row.ID, row.Topic, row.PartitionKey, row.Payload, mustMarshal(row.Headers), row.RegionID, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM outbox WHERE id = $1`, row.ID)
	return err
}

func mustMarshal(m map[string]string) []byte {
	if m == nil {
		m = map[string]string{}
	}
	b, _ := json.Marshal(m)
	return b
}

// Publisher 把一行 outbox 投递到下游（如 Kafka）。
//
// 实现者：见 services/feeding-svc/internal/outbox 的 Kafka 适配。
type Publisher interface {
	Publish(ctx context.Context, row OutboxRow) error
}

// RelayConfig relay worker 配置。
type RelayConfig struct {
	// BatchSize 每次抢占行数；默认 64。
	BatchSize int
	// Interval 空闲时轮询间隔；默认 1s。busy 时无 sleep。
	Interval time.Duration
	// MaxAttempts 单条行的最大重试次数；超过写入 outbox_dlq；默认 5。
	MaxAttempts int
	// Backoff 函数：给 attempt 序号返回 sleep；默认指数退避（min 100ms, max 2s）。
	Backoff func(attempt int) time.Duration
	// Concurrency relay 并发 worker 数；默认 1（已用 SKIP LOCKED，可线性扩展）。
	Concurrency int
}

// DefaultRelayConfig 默认配置。
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		BatchSize:   64,
		Interval:    1 * time.Second,
		MaxAttempts: 5,
		Backoff:     defaultBackoff,
		Concurrency: 1,
	}
}

func defaultBackoff(attempt int) time.Duration {
	d := 100 * time.Millisecond
	for i := 0; i < attempt && d < 2*time.Second; i++ {
		d *= 2
	}
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}

// Outbox relay 指标（package 级、按 service 共享）。
var (
	outboxPendingGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "yunmao_outbox_relay_pending",
		Help: "Pending outbox rows (sampled by relay worker).",
	})
	outboxPublishedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_outbox_relay_published_total",
		Help: "Outbox rows successfully published.",
	}, []string{"topic"})
	outboxPublishLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "yunmao_outbox_relay_publish_latency_seconds",
		Help:    "Latency of outbox publish (relay worker).",
		Buckets: prometheus.DefBuckets,
	}, []string{"topic"})
	outboxDLQTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_outbox_relay_dlq_total",
		Help: "Outbox rows moved to DLQ.",
	}, []string{"topic"})
	outboxAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_outbox_relay_attempts_total",
		Help: "Outbox publish attempts (success or failure).",
	}, []string{"topic", "result"})
)

// Relay 是 outbox 后台 worker。Start 启动一个或多个 goroutine。
type Relay struct {
	pool      *pgxpool.Pool
	publisher Publisher
	cfg       RelayConfig

	wg     sync.WaitGroup
	stopCh chan struct{}
	once   sync.Once
}

// NewRelay 构造。
func NewRelay(pool *pgxpool.Pool, pub Publisher, cfg RelayConfig) *Relay {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 64
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.Backoff == nil {
		cfg.Backoff = defaultBackoff
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	return &Relay{
		pool:      pool,
		publisher: pub,
		cfg:       cfg,
		stopCh:    make(chan struct{}),
	}
}

// Start 启动 worker；Stop 时统一回收。
func (r *Relay) Start(ctx context.Context) {
	for i := 0; i < r.cfg.Concurrency; i++ {
		r.wg.Add(1)
		go r.workerLoop(ctx)
	}
}

// Stop 通知所有 worker 退出并等待。
func (r *Relay) Stop() {
	r.once.Do(func() { close(r.stopCh) })
	r.wg.Wait()
}

func (r *Relay) workerLoop(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		default:
		}
		n, err := r.processBatch(ctx)
		if err != nil {
			// 记录但不退出；等下一轮
			outboxAttemptsTotal.WithLabelValues("_batch", "error").Inc()
		}
		if n == 0 {
			// idle：sleep then retry
			select {
			case <-time.After(r.cfg.Interval):
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			}
		}
	}
}

// processBatch 抢占并发布一批；返回成功条数。
//
// 错误处理：单行失败会重试到 MaxAttempts，超限写入 outbox_dlq。
func (r *Relay) processBatch(ctx context.Context) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := FetchUnpublished(ctx, tx, r.cfg.BatchSize)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		_ = tx.Rollback(ctx)
		// 也同时刷新一次 pending 计数（best-effort 单独事务里跑，避免持锁）。
		go r.refreshPending(context.Background())
		return 0, nil
	}

	publishedIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		ok := r.publishWithRetry(ctx, row)
		if ok {
			publishedIDs = append(publishedIDs, row.ID)
			outboxPublishedTotal.WithLabelValues(row.Topic).Inc()
		} else {
			// 超过 MaxAttempts；标记 DLQ
			if err := MarkDLQ(ctx, tx, row, "max attempts exceeded"); err != nil {
				return len(publishedIDs), fmt.Errorf("mark dlq: %w", err)
			}
			outboxDLQTotal.WithLabelValues(row.Topic).Inc()
		}
	}
	if err := MarkPublished(ctx, tx, publishedIDs); err != nil {
		return 0, fmt.Errorf("mark published: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(publishedIDs), nil
}

func (r *Relay) publishWithRetry(ctx context.Context, row OutboxRow) bool {
	for attempt := 0; attempt < r.cfg.MaxAttempts; attempt++ {
		start := time.Now()
		err := r.publisher.Publish(ctx, row)
		outboxPublishLatency.WithLabelValues(row.Topic).Observe(time.Since(start).Seconds())
		if err == nil {
			outboxAttemptsTotal.WithLabelValues(row.Topic, "ok").Inc()
			return true
		}
		outboxAttemptsTotal.WithLabelValues(row.Topic, "fail").Inc()
		// retry except last
		if attempt < r.cfg.MaxAttempts-1 {
			select {
			case <-time.After(r.cfg.Backoff(attempt)):
			case <-ctx.Done():
				return false
			case <-r.stopCh:
				return false
			}
		}
	}
	return false
}

func (r *Relay) refreshPending(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var n int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE published_at IS NULL`).Scan(&n); err == nil {
		outboxPendingGauge.Set(float64(n))
	}
}
