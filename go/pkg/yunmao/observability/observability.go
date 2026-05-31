// Package observability 提供共享的健康检查、就绪检查、metrics 路由。
//
// 第六轮（G 收尾）：引入分层探针：
//
//   - /healthz：legacy；与 /internal/livez 等价；仅 200 OK。
//   - /readyz：legacy；可选 ready 回调。
//   - /internal/livez：轻量存活，进程未崩即 200 OK；K8s livenessProbe 用。
//   - /internal/readyz：深度就绪；逐个 dep（PG/Redis/Kafka/MQTT/KeyProvider）调用 Probe，
//     任一失败返回 503 + 文本错误；K8s readinessProbe 用。
//   - /internal/keys/health：服务自定义，已在第五轮各 svc 路由表暴露。
//
// 用法（feeding-svc 等）：
//
//	probes := observability.Probes{
//	    "pg": pgPing,
//	    "kafka": kafkaPing,
//	}
//	observability.WireFull(r, probes)
package observability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Probe 单个依赖项探针；返回 nil 表示就绪。timeout 由调用方在闭包内自管。
type Probe func() error

// Probes 多依赖探针表；key 是依赖名（pg/redis/kafka/...）。
type Probes map[string]Probe

// ReadyCallback legacy /readyz 回调签名。
type ReadyCallback func() error

// Wire legacy：与上一轮兼容。新代码请用 WireFull。
func Wire(r chi.Router, ready ReadyCallback) {
	WireFull(r, Probes{}, ready)
}

// WireFull 把所有 health/ready/metrics 路由挂上。
//
// 行为：
//   - probes 不为 nil 时，/internal/readyz 调用所有探针；任一失败返回 503。
//   - legacy /readyz 仍然走 ready 回调（如有）。
//   - /internal/livez 永远 200 OK（仅证明进程活着）。
//   - /internal/readyz 输出 JSON：{ "ready": bool, "deps": { "pg": "ok|<err>", ... } }
func WireFull(r chi.Router, probes Probes, legacyReady ReadyCallback) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if legacyReady != nil {
			if err := legacyReady(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(err.Error()))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	r.Method(http.MethodGet, "/metrics", promhttp.Handler())

	r.Get("/internal/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/internal/readyz", func(w http.ResponseWriter, _ *http.Request) {
		out := struct {
			Ready bool              `json:"ready"`
			Deps  map[string]string `json:"deps"`
			TS    time.Time         `json:"ts"`
		}{
			Ready: true,
			Deps:  map[string]string{},
			TS:    time.Now().UTC(),
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for name, probe := range probes {
			name, probe := name, probe
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := safeProbe(probe)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					out.Ready = false
					out.Deps[name] = "err: " + err.Error()
				} else {
					out.Deps[name] = "ok"
				}
			}()
		}
		wg.Wait()
		w.Header().Set("Content-Type", "application/json")
		if !out.Ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

func safeProbe(p Probe) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("probe panic: %v", r)
		}
	}()
	return p()
}
