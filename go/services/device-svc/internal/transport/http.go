package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/httpx"
	"yunmao.live/pkg/yunmao/observability"

	feedingpb "yunmao.live/proto/feeding/v1"

	"yunmao.live/services/device-svc/internal/service"
)

// Config 设备服务路由配置。
type Config struct {
	FeedingGRPCAddr string
}

// New 构造 device-svc HTTP handler；probes 非空时启用 /internal/readyz 深度探针。
func New(svc *service.DeviceService, cfg Config, probes ...observability.Probes) http.Handler {
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

	for _, base := range []string{"/v1", "/api/v1", "/api/v1/admin"} {
		r.Route(base, func(r chi.Router) {
			r.Get("/devices", listDevices(svc))
			r.Post("/devices", registerDevice(svc))
			r.Get("/devices/{id}", getDevice(svc))
			r.Patch("/devices/{id}", updateDevice(svc))
			r.Post("/devices/{id}/bind", bindRoom(svc))
			r.Post("/devices/{id}/unbind", unbindRoom(svc))
			r.Post("/devices/{id}/status", setStatus(svc))
			r.Post("/devices/{id}/mqtt-credential", issueMqtt(svc))
			r.Post("/devices/{id}/firmware", updateFirmware(svc))
			r.Get("/devices/{id}/last-feed-request/{feedId}", lastFeedRequest(cfg))
		})
	}
	return r
}

func jwks(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, svc.JWKS())
	}
}

func keysHealth(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, svc.KeysHealth())
	}
}

func listDevices(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := service.ListFilter{
			OwnerID:  r.URL.Query().Get("owner_id"),
			RoomID:   r.URL.Query().Get("room_id"),
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
		devices, err := svc.List(r.Context(), f)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"devices": devices})
	}
}

func getDevice(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		d, err := svc.Get(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, d)
	}
}

func registerDevice(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var d service.Device
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		out, err := svc.Register(r.Context(), d)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

func updateDevice(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var d service.Device
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		d.ID = id
		out, err := svc.Update(r.Context(), d)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func bindRoom(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var body struct {
			RoomID string `json:"room_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if err := svc.BindRoom(r.Context(), id, body.RoomID); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "room_id": body.RoomID})
	}
}

func unbindRoom(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := svc.UnbindRoom(r.Context(), id); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "room_id": ""})
	}
}

func setStatus(svc *service.DeviceService) http.HandlerFunc {
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

func issueMqtt(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		cred, err := svc.IssueMqttCredential(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, cred)
	}
}

func updateFirmware(svc *service.DeviceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var body struct {
			Target string `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if err := svc.UpdateFirmware(r.Context(), id, body.Target); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "target": body.Target})
	}
}

func lastFeedRequest(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.FeedingGRPCAddr == "" {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "feeding gRPC not configured"))
			return
		}
		feedID := chi.URLParam(r, "feedId")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		conn, err := grpc.NewClient(cfg.FeedingGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "grpc dial: "+err.Error()))
			return
		}
		defer conn.Close()
		client := feedingpb.NewFeedingServiceClient(conn)
		resp, err := client.GetFeedRequest(ctx, &feedingpb.GetFeedRequestRequest{FeedRequestId: feedID})
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "grpc call: "+err.Error()))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"feed_request_id": resp.GetFeedRequestId(),
			"status":          resp.GetStatus().String(),
			"device_id":       resp.GetDeviceId(),
			"room_id":         resp.GetRoomId(),
			"amount_grams":    resp.GetAmountGrams(),
		})
	}
}
