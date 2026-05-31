package service

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	chatMessagesIn = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "chat_messages_in_total",
		Help: "弹幕入站消息总数（按 room_id 与 moderation_status）。",
	}, []string{"room_id", "status"})

	chatRatelimitRejects = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "chat_ratelimit_rejects_total",
		Help: "弹幕频控拒绝总数。",
	}, []string{"room_id", "reason"})

	chatModerationActions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "chat_moderation_total",
		Help: "弹幕审核动作总数（flagged/hidden/deleted/...）。",
	}, []string{"action"})
)
