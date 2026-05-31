// cat-feeder-sim CLI：基于 pkg/simdev 库的模拟猫舍出粮机入口。
//
// 库版本在 pkg/simdev/，e2e 测试与本 CLI 共享逻辑。新增 flag：
//
//	--cancel-loss-rate  故意忽略 cancel 命令的概率（0..1）；用于跑投喂 timeout 补偿路径。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"yunmao.live/clients/cat-feeder-sim/pkg/simdev"
)

var (
	devicesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cat_feeder_sim_devices_total",
		Help: "模拟设备总数",
	})
	feedCommands = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cat_feeder_sim_feed_commands_total",
		Help: "投喂事件计数（result=ack|done|failed|cancelled|cancel_lost）",
	}, []string{"result"})
	feedLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "cat_feeder_sim_feed_latency_seconds",
		Help:    "cmd→done 端到端 latency",
		Buckets: prometheus.ExponentialBuckets(0.05, 2, 8),
	})
	connectErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cat_feeder_sim_connect_errors_total",
		Help: "MQTT 连接失败总数",
	})
)

type flags struct {
	broker          string
	devices         int
	devicePrefix    string
	roomPrefix      string
	username        string
	password        string
	failRate        float64
	cancelLossRate  float64
	feedLatencyMs   string
	heartbeatSecs   int
	qos             int
	cleanSession    bool
	promListen      string
	connectJitterMs int
}

func parseFlags() *flags {
	f := &flags{}
	flag.StringVar(&f.broker, "broker", "tcp://localhost:1883", "MQTT broker 地址（多个用逗号分隔）")
	flag.IntVar(&f.devices, "devices", 1, "模拟设备数量")
	flag.StringVar(&f.devicePrefix, "device-prefix", "sim_dev_", "device_id 前缀")
	flag.StringVar(&f.roomPrefix, "room-prefix", "room_sim_", "room_id 前缀")
	flag.StringVar(&f.username, "username", "", "MQTT 用户名")
	flag.StringVar(&f.password, "password", "", "MQTT 密码")
	flag.Float64Var(&f.failRate, "fail-rate", 0.0, "故意回 failed 的概率（0-1）")
	flag.Float64Var(&f.cancelLossRate, "cancel-loss-rate", 0.0, "故意忽略 cancel cmd 的概率（用于跑 timeout 补偿路径）")
	flag.StringVar(&f.feedLatencyMs, "feed-latency-ms", "100-800", "出粮模拟延迟范围 ms（min-max）")
	flag.IntVar(&f.heartbeatSecs, "heartbeat-secs", 5, "心跳周期；0 表示禁用")
	flag.IntVar(&f.qos, "qos", 1, "MQTT QoS")
	flag.BoolVar(&f.cleanSession, "clean-session", true, "MQTT clean session")
	flag.StringVar(&f.promListen, "prom-listen", ":9301", "Prometheus 监听地址；空则不暴露")
	flag.IntVar(&f.connectJitterMs, "connect-jitter-ms", 200, "连接 jitter 上限")
	flag.Parse()
	return f
}

func parseLatencyRange(s string) (time.Duration, time.Duration, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expect format min-max, got %q", s)
	}
	lo, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	hi, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, err
	}
	if hi < lo {
		hi = lo
	}
	return time.Duration(lo) * time.Millisecond, time.Duration(hi) * time.Millisecond, nil
}

func main() {
	f := parseFlags()
	lo, hi, err := parseLatencyRange(f.feedLatencyMs)
	if err != nil {
		log.Fatalf("--feed-latency-ms invalid: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if f.promListen != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		go func() {
			srv := &http.Server{Addr: f.promListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			log.Printf("cat-feeder-sim prom /metrics on %s", f.promListen)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("prometheus listen: %v", err)
			}
		}()
	}

	brokers := strings.Split(f.broker, ",")

	hooks := simdev.Hooks{
		OnFeedAck:     func(string, simdev.FeedAckPayload) { feedCommands.WithLabelValues("ack").Inc() },
		OnFeedDone:    func(_ string, p simdev.FeedAckPayload, lat time.Duration) {
			if p.Status == "succeeded" {
				feedCommands.WithLabelValues("done").Inc()
			} else {
				feedCommands.WithLabelValues("failed").Inc()
			}
			feedLatency.Observe(lat.Seconds())
		},
		OnCancelAck:   func(string, simdev.FeedAckPayload) { feedCommands.WithLabelValues("cancelled").Inc() },
		OnFeedSkipped: func(_ string, reason string) { feedCommands.WithLabelValues(reason).Inc() },
		OnConnectErr:  func(string, error) { connectErrors.Inc() },
	}

	var wg sync.WaitGroup
	for i := 0; i < f.devices; i++ {
		wg.Add(1)
		devID := fmt.Sprintf("%s%06d", f.devicePrefix, i)
		roomID := fmt.Sprintf("%s%06d", f.roomPrefix, i)
		if f.connectJitterMs > 0 {
			time.Sleep(time.Duration(rand.Intn(f.connectJitterMs)) * time.Millisecond) //nolint:gosec
		}
		cfg := simdev.Config{
			DeviceID:       devID,
			RoomID:         roomID,
			Brokers:        brokers,
			Username:       f.username,
			Password:       f.password,
			CleanSession:   f.cleanSession,
			QoS:            byte(f.qos),
			HeartbeatEvery: time.Duration(f.heartbeatSecs) * time.Second,
			FeedLatencyMin: lo,
			FeedLatencyMax: hi,
			FailRate:       f.failRate,
			CancelLossRate: f.cancelLossRate,
			Hooks:          hooks,
			Logger:         func(format string, args ...any) { log.Printf(format, args...) },
		}
		go func(c simdev.Config) {
			defer wg.Done()
			d := simdev.New(c)
			devicesTotal.Inc()
			defer devicesTotal.Dec()
			if err := d.Run(ctx); err != nil {
				log.Printf("[%s] exit: %v", c.DeviceID, err)
			}
		}(cfg)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()
	log.Printf("cat-feeder-sim shutting down...")
	wg.Wait()
}
