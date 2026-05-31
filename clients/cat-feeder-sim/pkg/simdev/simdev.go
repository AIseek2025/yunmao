// Package simdev 是 cat-feeder-sim 的可被 import 的模拟设备库。
//
// e2e 测试与 CLI 复用同一份逻辑：连 MQTT、订阅 feed/cancel cmd、回 ack/done。
//
// 设计目标：
//
//   - 一个 Device = 一条 goroutine 跑生命周期；多 Device 共享 broker；
//   - 注入故障率 / cancel 丢失率，支持 e2e 跑 timeout 补偿路径；
//   - 不引入额外 Prometheus 依赖（CLI 自带）；包级 hook 由调用方提供。
package simdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// FeedCommandPayload 与 device-svc bridge.FeedCommandPayload 字段对齐。
type FeedCommandPayload struct {
	FeedRequestID   string `json:"feed_request_id"`
	DeviceCommandID string `json:"device_command_id"`
	DeviceID        string `json:"device_id"`
	RoomID          string `json:"room_id"`
	AmountGrams     uint32 `json:"amount_grams"`
	MotorDurationMs uint32 `json:"motor_duration_ms"`
	ExpiresAt       string `json:"expires_at"`
	Reason          string `json:"reason,omitempty"`
}

// FeedAckPayload 与 device-svc bridge.FeedAckPayload 字段对齐。
type FeedAckPayload struct {
	FeedRequestID      string   `json:"feed_request_id"`
	DeviceCommandID    string   `json:"device_command_id"`
	DeviceID           string   `json:"device_id"`
	RoomID             string   `json:"room_id"`
	Status             string   `json:"status"`
	ActualAmountGrams  uint32   `json:"actual_amount_grams"`
	RemainingFoodGrams uint32   `json:"remaining_food_grams"`
	ExecutedAt         string   `json:"executed_at"`
	Errors             []string `json:"errors,omitempty"`
}

// HeartbeatPayload 心跳报文。
type HeartbeatPayload struct {
	DeviceID           string `json:"device_id"`
	OnlineStatus       string `json:"online_status"`
	RemainingFoodGrams uint32 `json:"remaining_food_grams"`
	HealthStatus       string `json:"health_status"`
	Now                string `json:"now"`
}

// Hooks 让 CLI / 测试观察 Device 行为（指标 / 断言）。
type Hooks struct {
	OnFeedAck     func(deviceID string, payload FeedAckPayload)
	OnFeedDone    func(deviceID string, payload FeedAckPayload, latency time.Duration)
	OnCancelAck   func(deviceID string, payload FeedAckPayload)
	OnHeartbeat   func(deviceID string)
	OnConnectErr  func(deviceID string, err error)
	OnFeedSkipped func(deviceID, reason string)
}

// Config 单个 Device 配置。
type Config struct {
	DeviceID       string
	RoomID         string
	Brokers        []string
	Username       string
	Password       string
	CleanSession   bool
	QoS            byte
	HeartbeatEvery time.Duration

	// FeedLatencyMin/Max 出粮模拟延迟范围。
	FeedLatencyMin time.Duration
	FeedLatencyMax time.Duration

	// FailRate 故意回 failed 的概率（0..1）。
	FailRate float64
	// CancelLossRate 故意忽略 cancel 命令的概率（0..1）；触发后用于跑 timeout 补偿路径。
	CancelLossRate float64

	// InitialFoodGrams 初始剩余克数（不指定走 0..2000 随机）。
	InitialFoodGrams uint32

	// Hooks 回调。
	Hooks Hooks

	// Logger 函数式日志（默认 noop）。
	Logger func(format string, args ...any)
}

// Device 单个模拟设备实例。
type Device struct {
	cfg       Config
	rng       *rand.Rand
	remaining uint32
	mu        sync.Mutex
	cli       mqtt.Client
}

// New 构造，不连接。Run 会启动连接、订阅、心跳循环。
func New(cfg Config) *Device {
	if cfg.QoS == 0 {
		cfg.QoS = 1
	}
	if cfg.HeartbeatEvery == 0 {
		cfg.HeartbeatEvery = 5 * time.Second
	}
	if cfg.FeedLatencyMin == 0 {
		cfg.FeedLatencyMin = 100 * time.Millisecond
	}
	if cfg.FeedLatencyMax == 0 {
		cfg.FeedLatencyMax = 800 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string, ...any) {}
	}
	src := rand.NewSource(time.Now().UnixNano() + int64(len(cfg.DeviceID))) //nolint:gosec
	d := &Device{
		cfg:       cfg,
		rng:       rand.New(src), //nolint:gosec
		remaining: cfg.InitialFoodGrams,
	}
	if d.remaining == 0 {
		d.remaining = uint32(d.rng.Intn(2000))
	}
	return d
}

// Run 阻塞执行 Device 完整生命周期，ctx 取消即返回。
func (d *Device) Run(ctx context.Context) error {
	opts := mqtt.NewClientOptions()
	for _, b := range d.cfg.Brokers {
		opts.AddBroker(b)
	}
	opts.SetClientID(d.cfg.DeviceID).
		SetUsername(d.cfg.Username).
		SetPassword(d.cfg.Password).
		SetCleanSession(d.cfg.CleanSession).
		SetKeepAlive(30 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetConnectTimeout(5 * time.Second)

	cli := mqtt.NewClient(opts)
	tok := cli.Connect()
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		err := tok.Error()
		if err == nil {
			err = errors.New("mqtt connect timeout")
		}
		if d.cfg.Hooks.OnConnectErr != nil {
			d.cfg.Hooks.OnConnectErr(d.cfg.DeviceID, err)
		}
		return err
	}
	d.cli = cli
	defer cli.Disconnect(50)

	cmdTopic := fmt.Sprintf("device/%s/cmd/feed", d.cfg.DeviceID)
	cancelTopic := fmt.Sprintf("device/%s/cmd/cancel", d.cfg.DeviceID)
	ackTopic := fmt.Sprintf("device/%s/event/feed_ack", d.cfg.DeviceID)
	doneTopic := fmt.Sprintf("device/%s/event/feed_done", d.cfg.DeviceID)
	hbTopic := fmt.Sprintf("device/%s/event/heartbeat", d.cfg.DeviceID)
	cancelEvtTopic := fmt.Sprintf("device/%s/event/cancelled", d.cfg.DeviceID)

	if t := cli.Subscribe(cmdTopic, d.cfg.QoS, func(_ mqtt.Client, m mqtt.Message) {
		go d.handleFeed(ctx, m, ackTopic, doneTopic)
	}); !t.WaitTimeout(5*time.Second) || t.Error() != nil {
		return fmt.Errorf("subscribe %s: %w", cmdTopic, t.Error())
	}
	if t := cli.Subscribe(cancelTopic, d.cfg.QoS, func(_ mqtt.Client, m mqtt.Message) {
		go d.handleCancel(ctx, m, cancelEvtTopic)
	}); !t.WaitTimeout(5*time.Second) || t.Error() != nil {
		return fmt.Errorf("subscribe %s: %w", cancelTopic, t.Error())
	}

	d.publishHeartbeat(hbTopic)
	if d.cfg.Hooks.OnHeartbeat != nil {
		d.cfg.Hooks.OnHeartbeat(d.cfg.DeviceID)
	}
	ticker := time.NewTicker(d.cfg.HeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d.publishHeartbeat(hbTopic)
			if d.cfg.Hooks.OnHeartbeat != nil {
				d.cfg.Hooks.OnHeartbeat(d.cfg.DeviceID)
			}
		}
	}
}

func (d *Device) publishHeartbeat(topic string) {
	d.mu.Lock()
	remaining := d.remaining
	d.mu.Unlock()
	payload, _ := json.Marshal(HeartbeatPayload{
		DeviceID:           d.cfg.DeviceID,
		OnlineStatus:       "online",
		RemainingFoodGrams: remaining,
		HealthStatus:       "ok",
		Now:                time.Now().UTC().Format(time.RFC3339),
	})
	_ = d.cli.Publish(topic, d.cfg.QoS, false, payload).Wait()
}

func (d *Device) handleFeed(ctx context.Context, m mqtt.Message, ackTopic, doneTopic string) {
	t0 := time.Now()
	var cmd FeedCommandPayload
	if err := json.Unmarshal(m.Payload(), &cmd); err != nil {
		d.cfg.Logger("[%s] feed cmd parse err: %v", d.cfg.DeviceID, err)
		return
	}
	if cmd.DeviceID == "" {
		cmd.DeviceID = d.cfg.DeviceID
	}
	if cmd.RoomID == "" {
		cmd.RoomID = d.cfg.RoomID
	}

	ack := FeedAckPayload{
		FeedRequestID:   cmd.FeedRequestID,
		DeviceCommandID: cmd.DeviceCommandID,
		DeviceID:        cmd.DeviceID,
		RoomID:          cmd.RoomID,
		Status:          "acked",
		ExecutedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if b, err := json.Marshal(ack); err == nil {
		_ = d.cli.Publish(ackTopic, d.cfg.QoS, false, b).Wait()
		if d.cfg.Hooks.OnFeedAck != nil {
			d.cfg.Hooks.OnFeedAck(d.cfg.DeviceID, ack)
		}
	}

	span := d.cfg.FeedLatencyMax - d.cfg.FeedLatencyMin
	if span < time.Millisecond {
		span = time.Millisecond
	}
	delay := d.cfg.FeedLatencyMin + time.Duration(d.rng.Int63n(int64(span)))
	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}

	failed := d.rng.Float64() < d.cfg.FailRate
	status := "succeeded"
	var errs []string
	if failed {
		status = "failed"
		errs = []string{"motor_jammed"}
	} else {
		d.mu.Lock()
		if d.remaining >= cmd.AmountGrams {
			d.remaining -= cmd.AmountGrams
		} else {
			d.remaining = 0
		}
		d.mu.Unlock()
	}

	d.mu.Lock()
	remaining := d.remaining
	d.mu.Unlock()
	done := FeedAckPayload{
		FeedRequestID:      cmd.FeedRequestID,
		DeviceCommandID:    cmd.DeviceCommandID,
		DeviceID:           cmd.DeviceID,
		RoomID:             cmd.RoomID,
		Status:             status,
		ActualAmountGrams:  cmd.AmountGrams,
		RemainingFoodGrams: remaining,
		ExecutedAt:         time.Now().UTC().Format(time.RFC3339),
		Errors:             errs,
	}
	if b, err := json.Marshal(done); err == nil {
		_ = d.cli.Publish(doneTopic, d.cfg.QoS, false, b).Wait()
		if d.cfg.Hooks.OnFeedDone != nil {
			d.cfg.Hooks.OnFeedDone(d.cfg.DeviceID, done, time.Since(t0))
		}
	}
}

func (d *Device) handleCancel(_ context.Context, m mqtt.Message, evtTopic string) {
	var cmd FeedCommandPayload
	if err := json.Unmarshal(m.Payload(), &cmd); err != nil {
		d.cfg.Logger("[%s] cancel parse err: %v", d.cfg.DeviceID, err)
		return
	}
	if d.rng.Float64() < d.cfg.CancelLossRate {
		if d.cfg.Hooks.OnFeedSkipped != nil {
			d.cfg.Hooks.OnFeedSkipped(d.cfg.DeviceID, "cancel_lost")
		}
		return
	}
	ack := FeedAckPayload{
		FeedRequestID:   cmd.FeedRequestID,
		DeviceCommandID: cmd.DeviceCommandID,
		DeviceID:        d.cfg.DeviceID,
		RoomID:          cmd.RoomID,
		Status:          "cancelled",
		ExecutedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if b, err := json.Marshal(ack); err == nil {
		_ = d.cli.Publish(evtTopic, d.cfg.QoS, false, b).Wait()
		if d.cfg.Hooks.OnCancelAck != nil {
			d.cfg.Hooks.OnCancelAck(d.cfg.DeviceID, ack)
		}
	}
}

// Cluster 启动 N 个并发 Device，阻塞直到 ctx 取消。
func Cluster(ctx context.Context, configs []Config) error {
	var wg sync.WaitGroup
	for i := range configs {
		wg.Add(1)
		go func(c Config) {
			defer wg.Done()
			d := New(c)
			if err := d.Run(ctx); err != nil {
				c.Logger("[%s] device exit: %v", c.DeviceID, err)
			}
		}(configs[i])
	}
	wg.Wait()
	return nil
}
