// moderation_filter.go：把 internal/moderation.Manager 适配到 SensitiveFilter 接口。
//
// 设计要点（第七轮 F 收尾）：
//   - moderation.Manager.Inspect 返回 Decision{Action, CleanedBody, ...}
//   - SensitiveFilter.Check 期望 (clean, flagged, reason)；本适配器把
//     Action ∈ {hide, recall, block} 视为 flagged；warn 视为 flagged（仍展示但带提示）；
//     pass 视为非 flagged。
//   - chat-svc 当前调 Filter 仅区分 published vs flagged；recall/block 等动作
//     由 admin Moderate 路径补齐（参见 ADR-0022）。
package service

import (
	"context"
	"time"

	"yunmao.live/services/chat-svc/internal/moderation"
)

// ModerationManagerFilter 把 moderation.Manager 包成 SensitiveFilter。
type ModerationManagerFilter struct {
	m       *moderation.Manager
	timeout time.Duration
}

// NewModerationManagerFilter 构造。
func NewModerationManagerFilter(m *moderation.Manager, perCallTimeout time.Duration) *ModerationManagerFilter {
	if perCallTimeout <= 0 {
		perCallTimeout = 250 * time.Millisecond
	}
	return &ModerationManagerFilter{m: m, timeout: perCallTimeout}
}

// Check 适配。
func (f *ModerationManagerFilter) Check(text string) (string, bool, string) {
	if f == nil || f.m == nil {
		return text, false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()
	d := f.m.Inspect(ctx, text)
	switch d.Action {
	case moderation.ActionBlock, moderation.ActionHide, moderation.ActionRecall, moderation.ActionWarn:
		body := d.CleanedBody
		if body == "" {
			body = text
		}
		return body, true, d.Provider + ":" + d.Reason
	}
	if d.CleanedBody != "" {
		return d.CleanedBody, false, ""
	}
	return text, false, ""
}
