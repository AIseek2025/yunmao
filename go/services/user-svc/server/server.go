// Package server 把 user-svc 的 wiring（KeyProvider/Store/Signer）抽为可复用工厂。
package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/user-svc/internal/service"
	pgstore "yunmao.live/services/user-svc/internal/store"
	"yunmao.live/services/user-svc/internal/transport"
)

// Deps 注入 user-svc 工厂依赖。
type Deps struct {
	PG            *pgxpool.Pool
	MigrationsDir string
	KeyProvider   authjwt.KeyProvider
	TokenTTL      time.Duration
	Audience      string
}

// Server user-svc 工厂结果。
type Server struct {
	HTTP    http.Handler
	service *service.UserService
	Signer  *authjwt.Signer
	cleanup []func()
}

// New 构造。
func New(ctx context.Context, deps Deps) (*Server, error) {
	if deps.KeyProvider == nil {
		return nil, errors.New("user-svc: KeyProvider required")
	}
	if deps.TokenTTL == 0 {
		deps.TokenTTL = 24 * time.Hour
	}
	if deps.Audience == "" {
		deps.Audience = "yunmao.gateway"
	}
	signer, err := authjwt.NewSignerFromProvider(deps.KeyProvider, "yunmao.user-svc")
	if err != nil {
		return nil, err
	}

	var st pgstore.Store
	if deps.PG != nil {
		if deps.MigrationsDir != "" {
			if err := db.Apply(ctx, deps.PG, deps.MigrationsDir); err != nil {
				return nil, err
			}
		}
		st = pgstore.NewPgStore(deps.PG)
	}

	svc := service.New(service.Config{
		Signer:   signer,
		TokenTTL: deps.TokenTTL,
		Audience: deps.Audience,
		Store:    st,
	})
	probes := observability.Probes{}
	if deps.PG != nil {
		probes["pg"] = observability.PgProbe(deps.PG)
	}
	probes["keys"] = observability.KeysProbe(func() (string, error) {
		sk, err := deps.KeyProvider.Active()
		if err != nil {
			return "", err
		}
		return string(sk.Alg), nil
	})

	return &Server{
		HTTP:    transport.New(svc, probes),
		service: svc,
		Signer:  signer,
	}, nil
}

// Close 释放资源。
func (s *Server) Close() {
	for _, fn := range s.cleanup {
		fn()
	}
}

// ListenAndServe 启动 HTTP。
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
