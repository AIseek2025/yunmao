// Package server 把 admin-svc 的全部 wiring（safety/flag store + HTTP handler）
// 封装为可被 import 的工厂，供 cmd/admin-svc/main.go 与 e2e 测试共享。
package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/admin-svc/internal/service"
	"yunmao.live/services/admin-svc/internal/transport"
)

// Deps 注入 admin-svc 工厂的依赖。
type Deps struct {
	PG            *pgxpool.Pool
	MigrationsDir string
	Now           func() time.Time
	BillingBaseURL string
	RoomBaseURL    string
	Verifier      *authjwt.Verifier
	Signer        *authjwt.Signer
	AdminPassword string
}

// Server 是 admin-svc 工厂结果。
type Server struct {
	HTTP    http.Handler
	service *service.AdminService
	Safety  feedingsafety.Store
	Flags   featureflags.Store
	cleanup []func()
}

// New 构造 admin-svc。
func New(ctx context.Context, deps Deps) (*Server, error) {
	var (
		safety feedingsafety.Store
		flags  featureflags.Store
	)

	srv := &Server{}
	if deps.PG != nil {
		if deps.MigrationsDir != "" {
			if err := db.Apply(ctx, deps.PG, deps.MigrationsDir); err != nil {
				return nil, err
			}
		}
		safety = feedingsafety.NewPgStore(deps.PG, 0)
		flags = featureflags.NewPgStore(deps.PG)
	} else {
		safety = feedingsafety.NewMemoryStore()
		flags = featureflags.NewMemoryStore(
			featureflags.Flag{
				Name: "feed.global_kill_switch", Enabled: false, Scope: "global",
			},
			featureflags.Flag{
				Name: "billing.required", Enabled: false, Scope: "global",
			},
			featureflags.Flag{
				Name: "room.webrtc.enabled", Enabled: false, Scope: "global",
				Value: map[string]any{"gray_percent": 5.0},
			},
			featureflags.Flag{
				Name: "billing.feed_price_table", Enabled: false, Scope: "global",
				Value: map[string]any{
					"unit":           "fen_per_gram",
					"default":        5,
					"thresholds":     []any{},
					"updated_reason": "default-placeholder",
				},
			},
			featureflags.Flag{
				Name: "chat.moderation_provider", Enabled: true, Scope: "global",
				Value: map[string]any{"provider": "local"},
			},
		)
	}
	srv.Safety = safety
	srv.Flags = flags

	svc := service.New(safety, flags)
	srv.service = svc
	probes := observability.Probes{}
	if deps.PG != nil {
		probes["pg"] = observability.PgProbe(deps.PG)
	}
	srv.HTTP = transport.New(svc, transport.ProxyConfig{
		BillingBaseURL: deps.BillingBaseURL,
		RoomBaseURL:    deps.RoomBaseURL,
		Verifier:       deps.Verifier,
		Signer:         deps.Signer,
		AdminPassword:  deps.AdminPassword,
	}, probes)
	return srv, nil
}

func (s *Server) Close() {
	for _, fn := range s.cleanup {
		fn()
	}
}

// ListenAndServe 启动 HTTP；ctx 取消时 graceful shutdown（5s）。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	hs := &http.Server{Addr: addr, Handler: s.HTTP, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		err := hs.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shCtx)
		return nil
	case err := <-errCh:
		return err
	}
}
