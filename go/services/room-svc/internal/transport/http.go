package transport

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/httpx"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/room-svc/internal/service"
)

// New 路由器；probes 非空时启用 /internal/readyz 深度探针（ADR-0019/H 收尾）。
func New(svc *service.RoomService, allowGuest bool, probes ...observability.Probes) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.TraceMiddleware)
	p := observability.Probes{}
	for _, x := range probes {
		for k, v := range x {
			p[k] = v
		}
	}
	observability.WireFull(r, p, nil)

	r.Get("/jwks.json", jwks(svc))
	r.Get("/internal/keys/health", keysHealth(svc))

	for _, base := range []string{"/v1", "/api/v1"} {
		r.Route(base, func(r chi.Router) {
			r.Get("/rooms", listRooms(svc))
			r.Post("/rooms", createRoom(svc))
			r.Get("/rooms/{id}", getRoom(svc))
			r.Patch("/rooms/{id}", updateRoom(svc))
			r.Post("/rooms/{id}/status", setStatus(svc))
			r.Post("/rooms/{id}/rotate-stream-key", rotateStreamKey(svc))
			r.Post("/rooms/{id}/subscriptions", issueSubscription(svc, allowGuest))
			r.Get("/rooms/{id}/ice-servers", iceServers(svc))
		})
	}
	return r
}

func iceServers(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-User-Id")
		if userID == "" {
			userID = r.URL.Query().Get("user_id")
		}
		if userID == "" {
			// 房间订阅 token 也可携带 sub；优先 header / query。
			auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if auth != "" {
				userID = "anon-" + chi.URLParam(r, "id")
			} else {
				userID = "guest-" + chi.URLParam(r, "id")
			}
		}
		resp, err := svc.IssueIceServers(userID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}

func jwks(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, svc.JWKS())
	}
}

func keysHealth(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, svc.KeysHealth())
	}
}

func listRooms(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := service.ListFilter{
			OwnerID:  r.URL.Query().Get("owner_id"),
			RegionID: r.URL.Query().Get("region_id"),
			Status:   r.URL.Query().Get("status"),
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				f.Limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				f.Offset = n
			}
		}
		rooms, err := svc.List(r.Context(), f)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
	}
}

func getRoom(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		room, err := svc.Get(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, room)
	}
}

func createRoom(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var room service.Room
		if err := json.NewDecoder(r.Body).Decode(&room); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		out, err := svc.Create(r.Context(), room)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

func updateRoom(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var room service.Room
		if err := json.NewDecoder(r.Body).Decode(&room); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		room.ID = id
		out, err := svc.Update(r.Context(), room)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func setStatus(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if err := svc.SetStatus(r.Context(), id, body.Status); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": body.Status})
	}
}

func rotateStreamKey(svc *service.RoomService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		sk, err := svc.RotateStreamKey(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "stream_key": sk})
	}
}

func issueSubscription(svc *service.RoomService, allowGuest bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		auth := r.Header.Get("Authorization")
		userTok := ""
		if strings.HasPrefix(auth, "Bearer ") {
			userTok = strings.TrimPrefix(auth, "Bearer ")
		}
		resp, err := svc.IssueSubscription(r.Context(), service.SubscriptionRequest{
			RoomID:    id,
			UserToken: userTok,
		}, allowGuest)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}
