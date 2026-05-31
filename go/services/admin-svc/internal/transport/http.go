package transport

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"yunmao.live/pkg/yunmao/authjwt"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"
	"yunmao.live/pkg/yunmao/httpx"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/admin-svc/internal/service"
)

// ProxyConfig 注入各上游服务的 base URL 与 JWT verifier。
type ProxyConfig struct {
	BillingBaseURL string
	RoomBaseURL    string
	Verifier       *authjwt.Verifier
	Signer         *authjwt.Signer
	AdminPassword  string
}

func New(svc *service.AdminService, cfg ProxyConfig, probes observability.Probes) *chi.Mux {
	r := chi.NewRouter()
	r.Use(httpx.TraceMiddleware)
	observability.WireFull(r, probes, nil)

	mountAdmin := func(prefix string) {
		r.Route(prefix, func(r chi.Router) {
			if cfg.Verifier != nil {
				r.Use(RequireAdmin(cfg.Verifier))
			}
			r.Get("/rooms/{id}/feeding-policy", getPolicy(svc))
			r.Patch("/rooms/{id}/feeding-policy", updatePolicy(svc))

			r.Get("/feeding-safety", getGlobalSafety(svc))
			r.Put("/feeding-safety", putGlobalSafety(svc))

			r.Get("/feature-flags", listFlags(svc))
			r.Get("/feature-flags/{name}", getFlag(svc))
			r.Put("/feature-flags/{name}", setFlag(svc))

			r.Get("/webrtc/gray-sim", grayWebrtcSim(svc))

			r.Put("/chat/wordlist", putWordlist(svc))
			r.Get("/chat/wordlist", listWordlist(svc))
			r.Get("/chat/wordlist/version", wordlistVersion(svc))

			if cfg.BillingBaseURL != "" {
				r.Get("/wallets/{user_id}", proxyGetWallet(cfg.BillingBaseURL))
				r.Get("/wallets/holds/{hold_id}", proxyGetHold(cfg.BillingBaseURL))
			}

			if cfg.RoomBaseURL != "" {
				r.Get("/rooms", proxyListRooms(cfg.RoomBaseURL))
				r.Get("/rooms/{id}", proxyGetRoom(cfg.RoomBaseURL))
				r.Post("/rooms/{id}/rotate-stream-key", proxyRotateStreamKey(cfg.RoomBaseURL))
			}
		})
	}

	mountAdmin("/v1/admin")
	mountAdmin("/api/v1/admin")

	loginCfg := LoginConfig{
		AdminPassword: cfg.AdminPassword,
		Audience:      "yunmao.admin",
		TTL:           8 * time.Hour,
	}
	loginHandler := HandleAdminLogin(cfg.Signer, loginCfg)
	r.Post("/v1/auth/admin/login", loginHandler)
	r.Post("/api/v1/auth/admin/login", loginHandler)

	return r
}

func getPolicy(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := svc.GetPolicy(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, p)
	}
}

func updatePolicy(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p service.FeedingPolicy
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		p.RoomID = chi.URLParam(r, "id")
		if err := svc.UpdatePolicy(r.Context(), p); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type SafetyDTO struct {
	RoomCooldownSec     uint32 `json:"room_cooldown_sec"`
	UserRoomCooldownSec uint32 `json:"user_room_cooldown_sec"`
	CatDailyLimit       uint32 `json:"cat_daily_limit"`
}

func getGlobalSafety(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l, err := svc.GetGlobalSafety(r.Context())
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, SafetyDTO{
			RoomCooldownSec:     uint32(l.RoomCooldown / time.Second),
			UserRoomCooldownSec: uint32(l.UserRoomCooldown / time.Second),
			CatDailyLimit:       l.CatDailyLimit,
		})
	}
}

func putGlobalSafety(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var dto SafetyDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if err := svc.PutGlobalSafety(r.Context(), feedingsafety.Limits{
			RoomCooldown:     time.Duration(dto.RoomCooldownSec) * time.Second,
			UserRoomCooldown: time.Duration(dto.UserRoomCooldownSec) * time.Second,
			CatDailyLimit:    dto.CatDailyLimit,
		}); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type FlagDTO struct {
	Name      string         `json:"name"`
	Enabled   bool           `json:"enabled"`
	Scope     string         `json:"scope"`
	Value     map[string]any `json:"value"`
	UpdatedBy string         `json:"updated_by,omitempty"`
}

func listFlags(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.ListFlags(r.Context())
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, list)
	}
}

func getFlag(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		f, err := svc.GetFlag(r.Context(), name)
		if err != nil {
			if errors.Is(err, featureflags.ErrNotFound) {
				httpx.WriteError(w, r, yerr.New(yerr.RoomNotFound, "flag not found"))
				return
			}
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, f)
	}
}

func proxyGetWallet(base string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	base = strings.TrimRight(base, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "user_id")
		u := base + "/api/v1/wallets/" + userID
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
		resp, err := client.Do(req)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "billing: "+err.Error()))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

func proxyGetHold(base string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	base = strings.TrimRight(base, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		holdID := chi.URLParam(r, "hold_id")
		u := base + "/api/v1/wallets/holds/" + holdID
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
		resp, err := client.Do(req)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "billing: "+err.Error()))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

func proxyListRooms(base string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	base = strings.TrimRight(base, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		u, _ := url.Parse(base + "/v1/rooms")
		u.RawQuery = q.Encode()
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
		for _, h := range []string{"Authorization", "Cookie"} {
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "room-svc: "+err.Error()))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

func proxyGetRoom(base string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	base = strings.TrimRight(base, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		u := base + "/v1/rooms/" + id
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
		if v := r.Header.Get("Authorization"); v != "" {
			req.Header.Set("Authorization", v)
		}
		resp, err := client.Do(req)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "room-svc: "+err.Error()))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

func proxyRotateStreamKey(base string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	base = strings.TrimRight(base, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		u := base + "/v1/rooms/" + id + "/rotate-stream-key"
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, u, nil)
		if v := r.Header.Get("Authorization"); v != "" {
			req.Header.Set("Authorization", v)
		}
		resp, err := client.Do(req)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemDependencyUnavailable, "room-svc: "+err.Error()))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

type WordlistImportDTO struct {
	Entries   []service.WordlistEntry `json:"entries"`
	UpdatedBy string                  `json:"updated_by,omitempty"`
}

func putWordlist(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var dto WordlistImportDTO
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "text/csv") {
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			if err != nil {
				httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "body too large"))
				return
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				cols := strings.Split(line, ",")
				e := service.WordlistEntry{}
				if len(cols) > 0 {
					e.Region = strings.TrimSpace(cols[0])
				}
				if len(cols) > 1 {
					e.Language = strings.TrimSpace(cols[1])
				}
				if len(cols) > 2 {
					e.Word = strings.TrimSpace(cols[2])
				}
				if len(cols) > 3 {
					e.Action = strings.TrimSpace(cols[3])
				}
				if e.Word != "" {
					dto.Entries = append(dto.Entries, e)
				}
			}
			dto.UpdatedBy = r.Header.Get("X-Yunmao-Admin")
		} else {
			if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
				httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
				return
			}
		}
		ver, err := svc.ImportWordlist(r.Context(), dto.Entries, dto.UpdatedBy)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"version": ver,
			"applied": len(dto.Entries),
		})
	}
}

func listWordlist(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		region := r.URL.Query().Get("region")
		lang := r.URL.Query().Get("language")
		entries, err := svc.ListWordlist(r.Context(), region, lang)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"entries": entries,
		})
	}
}

func wordlistVersion(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := svc.WordlistVersion(r.Context())
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"version": v})
	}
}

func grayWebrtcSim(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("room_count"))
		out, err := svc.SimulateWebrtcGray(r.Context(), n)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func setFlag(svc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		var dto FlagDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if dto.Value == nil {
			dto.Value = map[string]any{}
		}
		if err := svc.SetFlag(r.Context(), featureflags.Flag{
			Name:      name,
			Enabled:   dto.Enabled,
			Scope:     dto.Scope,
			Value:     dto.Value,
			UpdatedBy: dto.UpdatedBy,
		}); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		f, err := svc.GetFlag(r.Context(), name)
		if err != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, f)
	}
}
