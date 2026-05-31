// Package mqttx 是 yunmao 平台对 MQTT v5 broker 的统一适配层。
//
// 目标：
//
//   - 把 device-svc 与具体 MQTT 客户端实现解耦：本地测试用 [`MemoryBroker`]，
//     生产 / 集成测试用 [`PahoClient`]（github.com/eclipse/paho.mqtt.golang）。
//   - 与 EMQX 5 / NanoMQ 默认 v3.1.1 + v5 兼容；MVP 走 v3.1.1，留 v5 升级口。
//   - 暴露简洁的 Subscribe / Publish / Connect / Disconnect 接口；不暴露 paho 私有类型。
//
// 一般用法（device-svc）：
//
//	cli, err := mqttx.Dial(ctx, mqttx.Config{
//	    Brokers: []string{"tcp://localhost:1883"},
//	    ClientID: "device-svc",
//	    Username: "device-svc",
//	    Password: "yunmao",
//	    OnDisconnect: ...,
//	})
//	cli.Subscribe(ctx, "device/+/event/+", 1, handler)
//	cli.Publish(ctx, "device/dev_demo/cmd/feed", 1, payload)
//
// 与 ADR-0012 对齐。
package mqttx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// QoS MQTT 服务质量等级。
type QoS byte

const (
	QoS0 QoS = 0
	QoS1 QoS = 1
	QoS2 QoS = 2
)

// Message 单条 MQTT 报文。
type Message struct {
	Topic   string
	Payload []byte
	QoS     QoS
	Retain  bool
}

// Handler 订阅回调；返回 error 仅用于 logging，不影响下次投递。
type Handler func(ctx context.Context, m Message) error

// Client 是 MQTT 客户端接口。Connect 必须在使用前调用一次。
type Client interface {
	// Connect 建立连接；阻塞至成功或超时。
	Connect(ctx context.Context) error
	// Disconnect 断开（quiesce 给定 ms）。
	Disconnect(quiesceMs uint) error
	// Subscribe 订阅 topic filter；wildcard 支持 + / #。
	Subscribe(ctx context.Context, filter string, qos QoS, h Handler) error
	// Publish 发布一条报文。
	Publish(ctx context.Context, topic string, qos QoS, payload []byte) error
	// IsConnected 当前是否在线。
	IsConnected() bool
}

// Config MQTT 客户端配置。
type Config struct {
	// Brokers 形如 ["tcp://localhost:1883"]。
	Brokers []string
	// ClientID 唯一；必填。
	ClientID string
	// Username / Password 可选；EMQX 默认有 ACL，建议总是设置。
	Username string
	Password string
	// CleanSession=true 时不保留订阅；MVP 推荐 true。
	CleanSession bool
	// KeepAlive 默认 30s。
	KeepAlive time.Duration
	// ConnectTimeout 默认 5s。
	ConnectTimeout time.Duration
	// AutoReconnect 默认 true。
	AutoReconnect bool
	// MaxReconnectInterval 默认 30s。
	MaxReconnectInterval time.Duration
	// OnConnect / OnDisconnect 状态回调，可为 nil。
	OnConnect    func()
	OnDisconnect func(err error)
}

// ErrNotConnected 客户端尚未连接。
var ErrNotConnected = errors.New("mqttx: not connected")

// --------------------------------------------------------------------------------------
// MemoryBroker —— 进程内 broker（仅供单测；不带持久化、ACL、QoS2）。
// --------------------------------------------------------------------------------------

// MemoryBroker 单进程内 broker；多个 MemoryClient 共享。
type MemoryBroker struct {
	mu   sync.RWMutex
	subs map[string][]*subscription // 精确 topic 列表（含 wildcard）
}

type subscription struct {
	clientID string
	filter   []string // 已分段
	h        Handler
}

// NewMemoryBroker 构造。
func NewMemoryBroker() *MemoryBroker {
	return &MemoryBroker{subs: map[string][]*subscription{}}
}

// NewClient 在 broker 上注册一个客户端。
func (b *MemoryBroker) NewClient(clientID string) *MemoryClient {
	return &MemoryClient{broker: b, clientID: clientID}
}

func (b *MemoryBroker) publish(ctx context.Context, topic string, payload []byte) {
	parts := splitTopic(topic)
	b.mu.RLock()
	var matches []Handler
	for _, list := range b.subs {
		for _, s := range list {
			if matchFilter(s.filter, parts) {
				matches = append(matches, s.h)
			}
		}
	}
	b.mu.RUnlock()
	for _, h := range matches {
		// 同步调用；测试场景足够。
		_ = h(ctx, Message{Topic: topic, Payload: append([]byte(nil), payload...), QoS: QoS1})
	}
}

func (b *MemoryBroker) subscribe(clientID, filter string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[clientID] = append(b.subs[clientID], &subscription{
		clientID: clientID,
		filter:   splitTopic(filter),
		h:        h,
	})
}

func (b *MemoryBroker) disconnect(clientID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, clientID)
}

// MemoryClient 在 MemoryBroker 上模拟一个 paho 客户端。
type MemoryClient struct {
	broker    *MemoryBroker
	clientID  string
	connected bool
}

func (c *MemoryClient) Connect(_ context.Context) error {
	c.connected = true
	return nil
}

func (c *MemoryClient) Disconnect(_ uint) error {
	c.broker.disconnect(c.clientID)
	c.connected = false
	return nil
}

func (c *MemoryClient) Subscribe(_ context.Context, filter string, _ QoS, h Handler) error {
	if !c.connected {
		return ErrNotConnected
	}
	c.broker.subscribe(c.clientID, filter, h)
	return nil
}

func (c *MemoryClient) Publish(ctx context.Context, topic string, _ QoS, payload []byte) error {
	if !c.connected {
		return ErrNotConnected
	}
	c.broker.publish(ctx, topic, payload)
	return nil
}

func (c *MemoryClient) IsConnected() bool { return c.connected }

// splitTopic 按 '/' 分段（保留空段）。
func splitTopic(t string) []string {
	if t == "" {
		return nil
	}
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(t); i++ {
		if t[i] == '/' {
			parts = append(parts, t[start:i])
			start = i + 1
		}
	}
	parts = append(parts, t[start:])
	return parts
}

// matchFilter 实现 MQTT topic filter 匹配（+ 单段通配；# 多段通配，只能末尾）。
func matchFilter(filter, parts []string) bool {
	i := 0
	for i < len(filter) && i < len(parts) {
		switch filter[i] {
		case "#":
			return true
		case "+":
			// match anything
		default:
			if filter[i] != parts[i] {
				return false
			}
		}
		i++
	}
	if i == len(filter) && i == len(parts) {
		return true
	}
	// 残留 "#" 通配
	if i < len(filter) && filter[i] == "#" {
		return true
	}
	return false
}

// --------------------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------------------

// DeviceEventTopic 设备上行事件 topic："device/{device_id}/event/{event_type}"
func DeviceEventTopic(deviceID, eventType string) string {
	return fmt.Sprintf("device/%s/event/%s", deviceID, eventType)
}

// DeviceCommandTopic 平台下发命令 topic："device/{device_id}/cmd/{cmd_type}"
func DeviceCommandTopic(deviceID, cmdType string) string {
	return fmt.Sprintf("device/%s/cmd/%s", deviceID, cmdType)
}
