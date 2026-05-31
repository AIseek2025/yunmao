package moderation

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	moderationCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_chat_moderation_calls_total",
		Help: "chat moderation calls by provider/outcome (ok|error|fallback|fallback_error).",
	}, []string{"provider", "outcome"})

	moderationLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "yunmao_chat_moderation_latency_seconds",
		Help:    "chat moderation call latency seconds by provider.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"provider"})
)
