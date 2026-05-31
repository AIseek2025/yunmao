// Package authjwt 中关于 HS256 下线流程的状态指示。
//
// ADR-0019：第六轮提供 `YUNMAO_ALLOW_HS256=true` 兼容窗口；
// 第七轮（本轮）正式删除 HS256 签发与校验路径：
//
//   - `NewHSKeyProvider` / `NewSigner([]byte,...)` / `NewVerifier([]byte)` 全部返回 [`ErrHS256Removed`]；
//   - `EnsureHS256Allowed` 始终返回 [`ErrHS256Removed`]；任何 main.go 在 alg=HS256 时
//     调用都会被快速失败，启动直接 panic / Fatalf；
//   - 保留 `AlgHS256` 常量与计数器，便于错误日志识别和监控历史峰值。
//
// 操作步骤参考 ADR-0019：第七轮归档。
package authjwt

import (
	"errors"
	"log"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// hs256AttemptCounter 仅用于监控 HS256 启动尝试（应稳定为 0）。
var hs256AttemptCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "yunmao_authjwt_hs256_usage_total",
	Help: "HS256 启动尝试次数（ADR-0019 已下线，正式版本应稳定为 0）。",
})

// ErrHS256Removed 表示 HS256 路径已在 ADR-0019 第七轮删除。
var ErrHS256Removed = errors.New("authjwt: HS256 path removed (ADR-0019, since 第七轮); use RS256/KMS")

// ErrHS256Disabled 历史名称；保留为 ErrHS256Removed 的别名，避免外部 callers 编译断裂。
//
// Deprecated: 用 ErrHS256Removed。
var ErrHS256Disabled = ErrHS256Removed

var warnOnce sync.Once

// EnsureHS256Allowed 永远拒绝 HS256；如 env `YUNMAO_ALLOW_HS256` 仍被设置，
// 仅打一次启动 warning，提示运维方完成清理，然后返回 ErrHS256Removed。
//
// 调用方（cmd/main.go）应：
//
//	if alg == "HS256" {
//	    if err := authjwt.EnsureHS256Allowed(); err != nil {
//	        log.Fatalf("%v", err)
//	    }
//	    // unreachable
//	}
func EnsureHS256Allowed() error {
	hs256AttemptCounter.Inc()
	if v := os.Getenv("YUNMAO_ALLOW_HS256"); v != "" {
		warnOnce.Do(func() {
			log.Printf("[authjwt] YUNMAO_ALLOW_HS256=%q is now ignored; HS256 path was removed in 第七轮 (ADR-0019). Remove this env from your deployment.", v)
		})
	}
	return ErrHS256Removed
}

// MustEnsureHS256Allowed 等价 EnsureHS256Allowed，失败 panic（仅供测试用）。
//
// Deprecated: 因为 EnsureHS256Allowed 永远返回错误，本函数总是 panic。
// 保留是为了避免外部 callers 编译断裂。
func MustEnsureHS256Allowed() {
	if err := EnsureHS256Allowed(); err != nil {
		panic(err)
	}
}
