// Package transport 提供 feeding-svc 的 HTTP 接入层。
package transport

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/httpx"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/feeding-svc/publisher"
	"yunmao.live/services/feeding-svc/internal/service"
)

// New 路由器：业务 + observability；probes 非空时启用 /internal/readyz 深度探针。
func New(svc *service.FeedingService, probes ...observability.Probes) http.Handler {
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
		r.Post("/feed-requests", createFeedRequest(svc))
		r.Get("/feed-requests/{id}", getFeedRequest(svc))
		r.Post("/feed-requests/{id}/cancel", cancelFeedRequest(svc))
	})

	r.Route("/internal", func(r chi.Router) {
		r.Post("/feed-acks", handleAck(svc))
	})
	return r
}

func cancelFeedRequest(svc *service.FeedingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var body struct {
			Reason string `json:"reason"`
		}
		// body 可选
		_ = json.NewDecoder(r.Body).Decode(&body)
		req, err := svc.Cancel(r.Context(), id, body.Reason)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, req)
	}
}

func createFeedRequest(svc *service.FeedingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		req, err := svc.Create(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, req)
	}
}

func getFeedRequest(svc *service.FeedingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		req, err := svc.Get(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, req)
	}
}

func handleAck(svc *service.FeedingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ack publisher.FeedCommandAcked
		if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if err := svc.HandleAck(r.Context(), ack); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
