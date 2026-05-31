package pay

import (
	"context"
	"testing"
	"time"
)

type fixedSource struct {
	orders []LocalOrder
}

func (f *fixedSource) ListOrdersForReconcile(_ context.Context, _ time.Time) ([]LocalOrder, error) {
	return f.orders, nil
}

func TestReconcileWorker_StatusMismatchEmitsDiff(t *testing.T) {
	reg := NewRegistry()
	mock := NewMockChannel(MockConfig{Secret: "k"})
	reg.Register(mock)
	// 模拟 mock 内部 status = paid
	mock.status["o1"] = "paid"
	mock.status["o2"] = "pending"

	src := &fixedSource{orders: []LocalOrder{
		{OrderID: "o1", Channel: ChannelMock, Status: "paid", AmountFen: 100},
		{OrderID: "o2", Channel: ChannelMock, Status: "paid", AmountFen: 200}, // 本地 paid，远端 pending → diff
	}}
	sink := NewInMemoryReconcileSink()
	w := NewReconcileWorker(src, reg, sink)
	w.SetInterval(time.Millisecond * 50)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(sink.Records()); got != 2 {
		t.Fatalf("expect 2 records got %d", got)
	}
	diffs := sink.Diffs()
	if len(diffs) != 1 {
		t.Fatalf("expect 1 diff got %d", len(diffs))
	}
	if diffs[0].OrderID != "o2" {
		t.Fatalf("expect o2 diff got %s", diffs[0].OrderID)
	}
}
