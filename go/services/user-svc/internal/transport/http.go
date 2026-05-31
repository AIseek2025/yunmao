package transport

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/httpx"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/user-svc/internal/service"
)

// New 构造 user-svc HTTP handler；probes 非空时启用 /internal/readyz 深度探针（ADR-0019/H 收尾）。
func New(svc *service.UserService, probes ...observability.Probes) http.Handler {
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

	r.Route("/v1", func(r chi.Router) {
		r.Post("/auth/login/sms", startLogin(svc))
		r.Post("/auth/login/sms/verify", completeLogin(svc))
		r.Post("/auth/login", devLogin(svc))
		r.Get("/users/{id}", getUser(svc))
	})

	// 兼容 /api/v1 旧前缀
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login/sms", startLogin(svc))
		r.Post("/auth/login/sms/verify", completeLogin(svc))
		r.Post("/auth/login", devLogin(svc))
		r.Get("/users/{id}", getUser(svc))
	})
	return r
}

func jwks(svc *service.UserService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, svc.JWKS())
	}
}

// keysHealth /internal/keys/health：暴露 active / retiring kid 与轮换信息。
// 若 KeyProvider 实现 Health() map[string]any，则透传；否则从 JWKS 衍生。
func keysHealth(svc *service.UserService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		out := svc.KeysHealth()
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func startLogin(svc *service.UserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PhoneE164 string `json:"phone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		id, code, exp, err := svc.StartSmsLogin(r.Context(), req.PhoneE164)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"challenge_id":       id,
			"sms_code":           code,
			"expires_in_seconds": exp.Seconds(),
		})
	}
}

func completeLogin(svc *service.UserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ChallengeID string `json:"challenge_id"`
			Code        string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		user, token, err := svc.CompleteSmsLogin(r.Context(), req.ChallengeID, req.Code)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"user":         user,
		})
	}
}

// devLogin POST /v1/auth/login
//
// 入参可包含 user_id 或 phone_e164；为开发联调 / 自动化测试设计。生产应禁用。
func devLogin(svc *service.UserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in service.LoginInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		user, token, err := svc.DevLogin(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"user":         user,
		})
	}
}

func getUser(svc *service.UserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		u, err := svc.Get(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, u)
	}
}
