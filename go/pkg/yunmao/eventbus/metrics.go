package eventbus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 暴露给 /metrics（已挂在每个服务的 observability.Wire）。
var (
	publishTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_eventbus_publish_total",
		Help: "Total number of CloudEvent envelopes published to a topic.",
	}, []string{"topic"})

	publishErrTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_eventbus_publish_errors_total",
		Help: "Total number of failed publishes (after retries).",
	}, []string{"topic"})

	dispatchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_eventbus_dispatch_total",
		Help: "Total number of CloudEvent envelopes successfully handled.",
	}, []string{"topic"})

	dispatchErrTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_eventbus_dispatch_errors_total",
		Help: "Total number of CloudEvent envelopes that failed all retries and were sent to DLQ.",
	}, []string{"topic"})
)
