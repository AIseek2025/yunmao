package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/httpx"
)

type claimsKey struct{}

func RequireAdmin(verifier *authjwt.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verifier == nil {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				httpx.WriteError(w, r, yerr.New(yerr.AuthLoginRequired, "missing bearer token"))
				return
			}
			claims, err := verifier.Parse(strings.TrimPrefix(auth, "Bearer "))
			if err != nil {
				httpx.WriteError(w, r, yerr.New(yerr.AuthTokenExpired, err.Error()))
				return
			}
			if claims.Scope != authjwt.ScopeAdmin {
				httpx.WriteError(w, r, yerr.New(yerr.AuthForbidden, "admin scope required"))
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AdminClaims(ctx context.Context) *authjwt.Claims {
	v, _ := ctx.Value(claimsKey{}).(*authjwt.Claims)
	return v
}

func AdminClaimsFromRouter(r *http.Request) *authjwt.Claims {
	return AdminClaims(r.Context())
}

// HandleAdminLogin issues an admin-scoped JWT after validating a shared admin password.
// Signer must be non-nil; password must match cfg.AdminPassword (empty password always rejected).
func HandleAdminLogin(signer *authjwt.Signer, cfg LoginConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if signer == nil || cfg.AdminPassword == "" {
			httpx.WriteError(w, r, yerr.New(yerr.AuthLoginRequired, "admin login unavailable"))
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if req.Password == "" || req.Password != cfg.AdminPassword {
			httpx.WriteError(w, r, yerr.New(yerr.AuthLoginRequired, "invalid credentials"))
			return
		}
		tok, err := signer.SignLogin("admin", authjwt.ScopeAdmin, cfg.Audience, cfg.TTL)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "sign token: "+err.Error()))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"access_token": tok,
			"expires_in":   int(cfg.TTL.Seconds()),
			"token_type":   "Bearer",
		})
	}
}

// LoginConfig configures the admin login endpoint.
type LoginConfig struct {
	AdminPassword string
	Audience      string
	TTL           time.Duration
}
