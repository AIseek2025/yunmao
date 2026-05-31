// Package httpx 提供共享 HTTP 中间件 / 工具：traceid、JSON 写入、错误响应。
package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	yerr "yunmao.live/pkg/yunmao/errors"
)

type ctxKey string

const traceIDKey ctxKey = "trace_id"

// TraceMiddleware 注入 X-Trace-Id（已有则透传，否则自动生成 UUIDv4）。
func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := r.Header.Get("X-Trace-Id")
		if trace == "" {
			trace = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), traceIDKey, trace)
		w.Header().Set("X-Trace-Id", trace)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TraceID 获取上下文中的 trace id；缺失则返回空字符串。
func TraceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return ""
}

// WriteJSON 输出 JSON。
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

// WriteError 用 yerr 错误码体系输出错误响应；自动带上 trace id。
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	app := yerr.AsAppError(err)
	app.WithTrace(TraceID(r.Context()))
	WriteJSON(w, app.Code.HTTPStatus(), app)
}
