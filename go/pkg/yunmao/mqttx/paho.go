package mqttx

import (
	"context"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// PahoClient 是基于 paho.mqtt.golang 的实现。
type PahoClient struct {
	cli     mqtt.Client
	cfg     Config
	mu      sync.Mutex
	subs    []struct {
		filter string
		qos    QoS
		h      Handler
	}
}

// Dial 构造并连接一个 Paho 客户端；遵循 cfg.ConnectTimeout 超时。
//
// 失败时返回连接错误，否则返回已连接的客户端。
func Dial(ctx context.Context, cfg Config) (Client, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("mqttx: client id required")
	}
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("mqttx: brokers required")
	}
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.KeepAlive == 0 {
		cfg.KeepAlive = 30 * time.Second
	}
	if cfg.MaxReconnectInterval == 0 {
		cfg.MaxReconnectInterval = 30 * time.Second
	}

	opts := mqtt.NewClientOptions()
	for _, b := range cfg.Brokers {
		opts.AddBroker(b)
	}
	opts.SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetCleanSession(cfg.CleanSession).
		SetKeepAlive(cfg.KeepAlive).
		SetConnectTimeout(cfg.ConnectTimeout).
		SetAutoReconnect(cfg.AutoReconnect).
		SetMaxReconnectInterval(cfg.MaxReconnectInterval)

	pc := &PahoClient{cfg: cfg}

	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		// 重连后重新订阅
		pc.mu.Lock()
		subs := append([]struct {
			filter string
			qos    QoS
			h      Handler
		}{}, pc.subs...)
		pc.mu.Unlock()
		for _, s := range subs {
			_ = pc.subscribe(s.filter, s.qos, s.h)
		}
		if cfg.OnConnect != nil {
			cfg.OnConnect()
		}
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		if cfg.OnDisconnect != nil {
			cfg.OnDisconnect(err)
		}
	})

	pc.cli = mqtt.NewClient(opts)
	if err := pc.Connect(ctx); err != nil {
		return nil, err
	}
	return pc, nil
}

// Connect 阻塞至连接成功；遵循 ctx + cfg.ConnectTimeout。
func (p *PahoClient) Connect(ctx context.Context) error {
	tok := p.cli.Connect()
	timeout := p.cfg.ConnectTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if !tok.WaitTimeout(timeout) {
		return fmt.Errorf("mqttx: connect timeout after %s", timeout)
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("mqttx: connect: %w", err)
	}
	select {
	case <-ctx.Done():
		_ = p.Disconnect(50)
		return ctx.Err()
	default:
	}
	return nil
}

// Disconnect 断开连接。
func (p *PahoClient) Disconnect(quiesceMs uint) error {
	p.cli.Disconnect(quiesceMs)
	return nil
}

// Subscribe 注册订阅；重连后由 onConnect 自动重订。
func (p *PahoClient) Subscribe(_ context.Context, filter string, qos QoS, h Handler) error {
	p.mu.Lock()
	p.subs = append(p.subs, struct {
		filter string
		qos    QoS
		h      Handler
	}{filter, qos, h})
	p.mu.Unlock()
	return p.subscribe(filter, qos, h)
}

func (p *PahoClient) subscribe(filter string, qos QoS, h Handler) error {
	tok := p.cli.Subscribe(filter, byte(qos), func(_ mqtt.Client, m mqtt.Message) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = h(ctx, Message{
			Topic:   m.Topic(),
			Payload: m.Payload(),
			QoS:     QoS(m.Qos()),
			Retain:  m.Retained(),
		})
	})
	tok.Wait()
	return tok.Error()
}

// Publish 发布。
func (p *PahoClient) Publish(_ context.Context, topic string, qos QoS, payload []byte) error {
	tok := p.cli.Publish(topic, byte(qos), false, payload)
	if !tok.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqttx: publish timeout topic=%s", topic)
	}
	return tok.Error()
}

// IsConnected 当前是否在线。
func (p *PahoClient) IsConnected() bool {
	return p.cli != nil && p.cli.IsConnected()
}
