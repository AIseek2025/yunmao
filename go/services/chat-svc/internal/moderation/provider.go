// Package moderation 弹幕审核 Provider 抽象 + 多家实现。
//
// 第七轮（F 收尾）：
//   - Provider interface：单一职责，输入文本返回 Decision；
//   - LocalProvider：本地词表 + 热更新接口；
//   - AliyunGreenProvider（mock）：阿里云内容安全 SDK 占位；凭据缺失时
//     由 Manager 自动 fallback 回 LocalProvider；
//   - Manager：根据 envvar / feature flag 选择 active provider，
//     记录 metrics（calls / latency / fallback_total），实现统一调用。
//
// 决策：见 ADR-0022 弹幕审核架构与撤回语义。
package moderation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// Action 审核动作。
//
// - "pass"   ：放行
// - "warn"   ：警告（消息仍展示）
// - "hide"   ：隐藏（消息打码 / 占位）
// - "recall" ：撤回（已展示消息从客户端移除）
// - "block"  ：直接屏蔽（不入库）
type Action string

const (
	ActionPass   Action = "pass"
	ActionWarn   Action = "warn"
	ActionHide   Action = "hide"
	ActionRecall Action = "recall"
	ActionBlock  Action = "block"
)

// Decision 审核结论。
type Decision struct {
	Action   Action  `json:"action"`
	Score    float64 `json:"score,omitempty"`
	Reason   string  `json:"reason,omitempty"`
	Provider string  `json:"provider"`
	// CleanedBody：provider 提供的安全文本（local provider 把命中词替换为 ***）。
	CleanedBody string `json:"cleaned_body,omitempty"`
}

// Provider 单一职责：检查文本并给出 Decision。
type Provider interface {
	Name() string
	Inspect(ctx context.Context, text string) (Decision, error)
}

// ----- Local provider -----

// LocalProvider 本地词表实现；支持 SetWords 热更新。
type LocalProvider struct {
	mu    sync.RWMutex
	words []string
}

// NewLocalProvider 用初始词表构造；nil = 内置默认。
func NewLocalProvider(words []string) *LocalProvider {
	if len(words) == 0 {
		words = DefaultWords()
	}
	return &LocalProvider{words: append([]string(nil), words...)}
}

// SetWords 热更新词表（admin 调用）。
func (l *LocalProvider) SetWords(words []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.words = append([]string(nil), words...)
}

// Name returns provider 名。
func (l *LocalProvider) Name() string { return "local" }

// Inspect 按词表逐项匹配；命中即返回 Hide（保守策略）。
func (l *LocalProvider) Inspect(_ context.Context, text string) (Decision, error) {
	l.mu.RLock()
	words := l.words
	l.mu.RUnlock()
	lower := strings.ToLower(text)
	for _, w := range words {
		if w == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(w)) {
			masked := strings.ReplaceAll(text, w, strings.Repeat("*", len([]rune(w))))
			return Decision{
				Action:      ActionHide,
				Reason:      "sensitive_word",
				Provider:    "local",
				Score:       1.0,
				CleanedBody: masked,
			}, nil
		}
	}
	return Decision{Action: ActionPass, Provider: "local", CleanedBody: text}, nil
}

// DefaultWords 内置最小词表（用于离线 dev / CI）。
func DefaultWords() []string {
	return []string{
		"草你妈", "操你妈", "傻逼", "fuck", "shit", "spam",
	}
}

// ----- Aliyun Green provider (mock) -----

// AliyunGreenConfig 阿里云内容安全 SDK 凭据。
type AliyunGreenConfig struct {
	AccessKey    string
	AccessSecret string
	Region       string // cn-shanghai / cn-hangzhou
	// Endpoint：可选，覆盖默认 https://green.{region}.aliyuncs.com（dev / 测试用）。
	Endpoint string
	// MockMode：true = 不调真实 SDK；CI / dev 默认 true。
	MockMode bool
}

// AliyunGreenProvider 阿里云内容安全 SDK 客户端。
//
// 真实 SDK：github.com/aliyun/alibaba-cloud-sdk-go/services/green。
// 当前实现是 mock —— 当文本包含 "aliyun-block-test" 时返回 Block，
// 包含 "aliyun-warn-test" 时返回 Warn，否则返回 Pass。
// 这样既能跑 CI，又能从黑盒视角验证 Manager 的 fallback / 切换逻辑。
type AliyunGreenProvider struct {
	cfg AliyunGreenConfig
}

// NewAliyunGreenProvider 构造；凭据缺失时返回 error，调用方应 fallback 到 LocalProvider。
func NewAliyunGreenProvider(cfg AliyunGreenConfig) (*AliyunGreenProvider, error) {
	if !cfg.MockMode {
		if cfg.AccessKey == "" || cfg.AccessSecret == "" {
			return nil, errors.New("moderation/aliyun_green: ak/sk required when MockMode=false")
		}
	}
	return &AliyunGreenProvider{cfg: cfg}, nil
}

// Name returns provider 名。
func (a *AliyunGreenProvider) Name() string { return "aliyun_green" }

// Inspect mock：基于关键字模式触发不同 action。
func (a *AliyunGreenProvider) Inspect(_ context.Context, text string) (Decision, error) {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "aliyun-block-test"):
		return Decision{Action: ActionBlock, Reason: "policy:block_mock", Provider: "aliyun_green", Score: 0.99, CleanedBody: text}, nil
	case strings.Contains(lower, "aliyun-warn-test"):
		return Decision{Action: ActionWarn, Reason: "policy:warn_mock", Provider: "aliyun_green", Score: 0.6, CleanedBody: text}, nil
	}
	return Decision{Action: ActionPass, Provider: "aliyun_green", CleanedBody: text}, nil
}

// ----- Manager -----

// Manager 多 provider 统一入口；支持 fallback 到 local + 动态切换。
type Manager struct {
	mu       sync.RWMutex
	primary  Provider
	fallback Provider // 一般是 LocalProvider
	timeout  time.Duration
}

// NewManager 构造；fallback 必填（推荐 LocalProvider）。
func NewManager(primary, fallback Provider, callTimeout time.Duration) *Manager {
	if fallback == nil {
		fallback = NewLocalProvider(nil)
	}
	if primary == nil {
		primary = fallback
	}
	if callTimeout <= 0 {
		callTimeout = 250 * time.Millisecond
	}
	return &Manager{primary: primary, fallback: fallback, timeout: callTimeout}
}

// SetPrimary 热切 primary provider（admin 调用）。
func (m *Manager) SetPrimary(p Provider) {
	if p == nil {
		return
	}
	m.mu.Lock()
	m.primary = p
	m.mu.Unlock()
}

// Inspect 调 primary；失败或超时则降级到 fallback；同时打 metrics。
func (m *Manager) Inspect(ctx context.Context, text string) Decision {
	m.mu.RLock()
	p := m.primary
	fb := m.fallback
	to := m.timeout
	m.mu.RUnlock()

	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	t0 := time.Now()
	decision, err := p.Inspect(cctx, text)
	moderationLatency.WithLabelValues(p.Name()).Observe(time.Since(t0).Seconds())
	if err == nil {
		moderationCalls.WithLabelValues(p.Name(), "ok").Inc()
		return decision
	}
	moderationCalls.WithLabelValues(p.Name(), "error").Inc()

	if fb == nil {
		moderationCalls.WithLabelValues("none", "fallback").Inc()
		return Decision{Action: ActionPass, Provider: "none", Reason: "no-fallback:" + err.Error(), CleanedBody: text}
	}
	t1 := time.Now()
	d2, err2 := fb.Inspect(ctx, text)
	moderationLatency.WithLabelValues(fb.Name()).Observe(time.Since(t1).Seconds())
	if err2 != nil {
		moderationCalls.WithLabelValues(fb.Name(), "fallback_error").Inc()
		return Decision{Action: ActionPass, Provider: fb.Name(), Reason: "fallback-error:" + err2.Error(), CleanedBody: text}
	}
	d2.Reason = "fallback:" + p.Name() + ":" + err.Error()
	moderationCalls.WithLabelValues(fb.Name(), "fallback").Inc()
	return d2
}

// Active 返回当前 primary provider name（用于 /internal/diagnose）。
func (m *Manager) Active() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.primary.Name()
}
