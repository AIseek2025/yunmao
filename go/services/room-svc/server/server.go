// Package server 把 room-svc 的 wiring 提取为可复用工厂。
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
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/room-svc/internal/service"
	"yunmao.live/services/room-svc/internal/store"
	"yunmao.live/services/room-svc/internal/transport"
	"yunmao.live/services/room-svc/internal/turn"
)

// Deps 注入 room-svc 工厂依赖。
type Deps struct {
	PG               *pgxpool.Pool
	MigrationsDir    string
	KeyProvider      authjwt.KeyProvider
	VerifierProvider authjwt.KeyProvider // 默认 = KeyProvider
	TokenTTL         time.Duration
	Audience         string
	RegionID         string
	StreamHMACSecret []byte
	AllowGuest       bool
	// TURN 凭证签发；以下三项任一为空时 GetIceServers 仅返回 STUN。
	TurnPrimarySecret []byte
	TurnLegacySecret  []byte // 轮换期接受旧 secret 校验
	TurnHosts         []string
	TurnPorts         []int
	StunUrls          []string
	TurnTTL           time.Duration
	// Flags 非空时启用灰度策略；通常 main.go 用 PG 或 Memory store + Manager 注入。
	Flags *featureflags.Manager
}

// Server room-svc 工厂结果。
type Server struct {
	HTTP    http.Handler
	service *service.RoomService
	Signer  *authjwt.Signer
	cleanup []func()
}

// New 构造。
func New(ctx context.Context, deps Deps) (*Server, error) {
	if deps.KeyProvider == nil {
		return nil, errors.New("room-svc: KeyProvider required")
	}
	if deps.VerifierProvider == nil {
		deps.VerifierProvider = deps.KeyProvider
	}
	if deps.TokenTTL == 0 {
		deps.TokenTTL = 10 * time.Minute
	}
	if deps.Audience == "" {
		deps.Audience = "yunmao.gateway"
	}
	if deps.RegionID == "" {
		deps.RegionID = "global"
	}

	signer, err := authjwt.NewSignerFromProvider(deps.KeyProvider, "yunmao.room-svc")
	if err != nil {
		return nil, err
	}
	verifier, err := authjwt.NewVerifierFromProvider(deps.VerifierProvider)
	if err != nil {
		return nil, err
	}

	var st store.Store = store.NewMemoryStore()
	if deps.PG != nil {
		if deps.MigrationsDir != "" {
			if err := db.Apply(ctx, deps.PG, deps.MigrationsDir); err != nil {
				return nil, err
			}
		}
		st = store.NewPgStore(deps.PG)
	}

	var turnSigner *turn.Signer
	if len(deps.TurnPrimarySecret) > 0 && len(deps.TurnHosts) > 0 {
		ts, err := turn.NewSigner(turn.Config{
			PrimarySecret: deps.TurnPrimarySecret,
			LegacySecret:  deps.TurnLegacySecret,
		})
		if err != nil {
			return nil, err
		}
		turnSigner = ts
	}

	flags := deps.Flags
	if flags == nil {
		// 默认 memory + room.webrtc.enabled=false。
		flags = featureflags.NewManager(featureflags.Config{
			Store: featureflags.NewMemoryStore(featureflags.Flag{
				Name:    "room.webrtc.enabled",
				Enabled: false,
				Scope:   "global",
				Value:   map[string]any{"gray_percent": 0.0},
			}),
		})
	}

	svc := service.New(service.Config{
		Signer:           signer,
		Verifier:         verifier,
		TokenTTL:         deps.TokenTTL,
		Audience:         deps.Audience,
		Store:            st,
		RegionID:         deps.RegionID,
		StreamHMACSecret: deps.StreamHMACSecret,
		TurnSigner:       turnSigner,
		TurnHosts:        deps.TurnHosts,
		TurnPorts:        deps.TurnPorts,
		StunUrls:         deps.StunUrls,
		TurnTTL:          deps.TurnTTL,
		Flags:            flags,
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
		HTTP:    transport.New(svc, deps.AllowGuest, probes),
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
