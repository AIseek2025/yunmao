// Package transport chat-svc HTTP 接入。
package transport

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/httpx"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/chat-svc/internal/service"
)

// New 构造 chat-svc HTTP handler；probes 非空时启用 /internal/readyz 深度探针。
func New(svc *service.ChatService, probes ...observability.Probes) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.TraceMiddleware)
	p := observability.Probes{}
	for _, x := range probes {
		for k, v := range x {
			p[k] = v
		}
	}
	observability.WireFull(r, p, nil)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/rooms/{room_id}/chat", send(svc))
		r.Get("/rooms/{room_id}/chat", list(svc))
		r.Post("/admin/chat/messages/{id}/moderate", moderate(svc))
	})
	return r
}

func send(svc *service.ChatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in service.SendInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		in.RoomID = chi.URLParam(r, "room_id")
		// user_id 由 gateway / 上游网关注入 X-User-Id header，PoC 兜底接受 body。
		if uid := r.Header.Get("X-User-Id"); uid != "" {
			in.UserID = uid
		}
		out, err := svc.Send(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

func list(svc *service.ChatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := svc.List(r.Context(), chi.URLParam(r, "room_id"), limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func moderate(svc *service.ChatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		out, err := svc.Moderate(r.Context(), chi.URLParam(r, "id"), body.Status, body.Reason)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}
