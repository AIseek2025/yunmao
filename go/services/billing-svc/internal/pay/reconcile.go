package pay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ReconcileRecord 单条对账结果。
type ReconcileRecord struct {
	RunID           string
	Channel         Channel
	OrderID         string
	ExternalTradeNo string
	LocalStatus     string
	RemoteStatus    string
	LocalAmountFen  int64
	RemoteAmountFen int64
	DiffReason      string // 空 = 一致
	CreatedAt       time.Time
}

// LocalOrder reconcile worker 从本地拿到的订单视图。
type LocalOrder struct {
	OrderID   string
	Channel   Channel
	Status    string
	AmountFen int64
}

// LocalOrderSource 给 worker 用的本地订单源（service 层适配）。
type LocalOrderSource interface {
	ListOrdersForReconcile(ctx context.Context, since time.Time) ([]LocalOrder, error)
}

// ReconcileSink 写对账记录 + 发事件（生产 → PG + Kafka）。
type ReconcileSink interface {
	WriteRecord(ctx context.Context, rec ReconcileRecord) error
	EmitDiff(ctx context.Context, rec ReconcileRecord) error
}

// InMemoryReconcileSink 测试用 sink；线程安全。
type InMemoryReconcileSink struct {
	mu      sync.Mutex
	records []ReconcileRecord
	diffs   []ReconcileRecord
}

// NewInMemoryReconcileSink 构造。
func NewInMemoryReconcileSink() *InMemoryReconcileSink {
	return &InMemoryReconcileSink{}
}

// WriteRecord 累积所有记录（一致与否）。
func (s *InMemoryReconcileSink) WriteRecord(_ context.Context, rec ReconcileRecord) error {
	s.mu.Lock()
	s.records = append(s.records, rec)
	s.mu.Unlock()
	return nil
}

// EmitDiff 仅累积 diff 记录。
func (s *InMemoryReconcileSink) EmitDiff(_ context.Context, rec ReconcileRecord) error {
	s.mu.Lock()
	s.diffs = append(s.diffs, rec)
	s.mu.Unlock()
	return nil
}

// Records 返回累积的记录（测试断言）。
func (s *InMemoryReconcileSink) Records() []ReconcileRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ReconcileRecord, len(s.records))
	copy(out, s.records)
	return out
}

// Diffs 返回累积的 diff 列表。
func (s *InMemoryReconcileSink) Diffs() []ReconcileRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ReconcileRecord, len(s.diffs))
	copy(out, s.diffs)
	return out
}

// ReconcileWorker 每 interval 跑一次对账：拉本地订单 → 调各渠道 QueryStatus → 对比 → 落盘。
type ReconcileWorker struct {
	interval time.Duration
	src      LocalOrderSource
	registry *Registry
	sink     ReconcileSink
	lookback time.Duration
	cancel   context.CancelFunc
}

// NewReconcileWorker 构造；interval 缺省 1h，lookback 缺省 24h。
func NewReconcileWorker(src LocalOrderSource, reg *Registry, sink ReconcileSink) *ReconcileWorker {
	return &ReconcileWorker{
		interval: time.Hour,
		src:      src,
		registry: reg,
		sink:     sink,
		lookback: 24 * time.Hour,
	}
}

// SetInterval 覆盖默认间隔（测试用更短）。
func (w *ReconcileWorker) SetInterval(d time.Duration) { w.interval = d }

// SetLookback 覆盖回溯窗口。
func (w *ReconcileWorker) SetLookback(d time.Duration) { w.lookback = d }

// Run 启动 ticker；ctx Done 时退出。
func (w *ReconcileWorker) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	go func() {
		// 启动即跑一轮
		_ = w.RunOnce(ctx)
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = w.RunOnce(ctx)
			}
		}
	}()
}

// Stop 停止 worker。
func (w *ReconcileWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

// RunOnce 执行一轮对账；返回处理订单数。
func (w *ReconcileWorker) RunOnce(ctx context.Context) error {
	runID := fmt.Sprintf("reco_%d", time.Now().Unix())
	orders, err := w.src.ListOrdersForReconcile(ctx, time.Now().Add(-w.lookback))
	if err != nil {
		return err
	}
	for _, o := range orders {
		ch, err := w.registry.Get(o.Channel)
		if err != nil {
			// 渠道未注册：跳过（生产应告警）。
			continue
		}
		q, err := ch.QueryStatus(ctx, o.OrderID)
		var remoteStatus string
		var remoteAmount int64
		if err == nil && q != nil {
			remoteStatus = q.Status
			remoteAmount = q.AmountFen
		} else if err != nil {
			remoteStatus = "query_error:" + err.Error()
		}
		diff := ""
		if remoteStatus != o.Status && remoteStatus != "" {
			diff = "status_mismatch"
		}
		if remoteAmount > 0 && remoteAmount != o.AmountFen {
			if diff == "" {
				diff = "amount_mismatch"
			} else {
				diff += ",amount_mismatch"
			}
		}
		rec := ReconcileRecord{
			RunID:           runID,
			Channel:         o.Channel,
			OrderID:         o.OrderID,
			LocalStatus:     o.Status,
			RemoteStatus:    remoteStatus,
			LocalAmountFen:  o.AmountFen,
			RemoteAmountFen: remoteAmount,
			DiffReason:      diff,
			CreatedAt:       time.Now().UTC(),
		}
		_ = w.sink.WriteRecord(ctx, rec)
		if diff != "" {
			_ = w.sink.EmitDiff(ctx, rec)
		}
	}
	return nil
}

// MarshalForLog 把对账记录序列化为 JSON 行（PG 写入 / 事件 payload 共用）。
func MarshalForLog(rec ReconcileRecord) ([]byte, error) {
	return json.Marshal(map[string]any{
		"run_id":            rec.RunID,
		"channel":           string(rec.Channel),
		"order_id":          rec.OrderID,
		"external_trade_no": rec.ExternalTradeNo,
		"local_status":      rec.LocalStatus,
		"remote_status":     rec.RemoteStatus,
		"local_amount_fen":  rec.LocalAmountFen,
		"remote_amount_fen": rec.RemoteAmountFen,
		"diff_reason":       rec.DiffReason,
		"created_at":        rec.CreatedAt.Format(time.RFC3339Nano),
	})
}
