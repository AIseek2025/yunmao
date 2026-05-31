// Package feedstate 实现 04 章第 4 节的投喂状态机。
//
// 状态：
//
//	created → rejected
//	         → accepted → queued → dispatched → acknowledged → succeeded
//	                                                         → failed → compensated
//
// 这里的状态机只负责合法迁移校验；持久化由 feeding-svc 自己负责。
package feedstate

import (
	"errors"
	"fmt"
)

type State string

const (
	Created      State = "created"
	Rejected     State = "rejected"
	Accepted     State = "accepted"
	Queued       State = "queued"
	Dispatched   State = "dispatched"
	Acknowledged State = "acknowledged"
	Succeeded    State = "succeeded"
	Failed       State = "failed"
	Compensated  State = "compensated"
)

// allowedTransitions 描述合法的状态迁移。
//
// 第四轮新增：
//   - queued / dispatched → rejected（用户取消 cancel 路径）
//
// 注意：rejected 终态在调用方语义上区分原因（reject_reason 字段）：
//   - "cancelled" 表示用户主动取消（feeding-svc /cancel）
//   - 其他值表示业务校验拒绝。
var allowedTransitions = map[State]map[State]bool{
	Created:      {Rejected: true, Accepted: true},
	Accepted:     {Queued: true, Rejected: true},
	Queued:       {Dispatched: true, Failed: true, Rejected: true},
	Dispatched:   {Acknowledged: true, Failed: true, Rejected: true},
	Acknowledged: {Succeeded: true, Failed: true},
	Succeeded:    {},
	Failed:       {Compensated: true},
	Rejected:     {},
	Compensated:  {},
}

// IsTerminal 是否终态。
func (s State) IsTerminal() bool {
	switch s {
	case Rejected, Succeeded, Compensated:
		return true
	}
	// Failed 也视为可终结，但允许过渡到 compensated。
	return false
}

// ErrInvalidTransition 非法迁移。
var ErrInvalidTransition = errors.New("invalid feed state transition")

// Transition 校验并返回 next 状态；如果非法返回错误。
func Transition(from, to State) (State, error) {
	dests, ok := allowedTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: unknown from %q", ErrInvalidTransition, from)
	}
	if !dests[to] {
		return "", fmt.Errorf("%w: %s -> %s not allowed", ErrInvalidTransition, from, to)
	}
	return to, nil
}

// MustTransition 用于已知合法的场景；非法 panic（一般用于内部）。
func MustTransition(from, to State) State {
	s, err := Transition(from, to)
	if err != nil {
		panic(err)
	}
	return s
}
