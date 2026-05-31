package service

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	feedRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_feed_requests_total",
		Help: "Total number of feed requests submitted to feeding-svc.",
	}, []string{"outcome"})

	feedStateTransitionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_feed_state_transitions_total",
		Help: "Total number of feed-state transitions emitted by feeding-svc.",
	}, []string{"to"})

	feedCooldownBlockedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_feed_cooldown_blocked_total",
		Help: "Total number of feed requests blocked by cooldowns / daily limits.",
	}, []string{"reason"})

	outboxListenerOKTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_feed_outbox_listener_ok_total",
		Help: "feeding-svc outbox listener writes that succeeded.",
	}, []string{"op"})

	outboxListenerErrTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yunmao_feed_outbox_listener_err_total",
		Help: "feeding-svc outbox listener writes that failed.",
	}, []string{"op"})
)
