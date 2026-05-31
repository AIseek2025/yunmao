// Package server 把 billing-svc 的 wiring 提取为可复用工厂。
package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/billing-svc/internal/pay"
	"yunmao.live/services/billing-svc/internal/service"
	"yunmao.live/services/billing-svc/internal/store"
	"yunmao.live/services/billing-svc/internal/transport"
)

// Deps 注入 billing-svc 工厂的依赖。
type Deps struct {
	PG            *pgxpool.Pool
	MigrationsDir string
	// Bus 非空时启动 outbox relay（要求 PG 也非空）。
	Bus eventbus.Bus

	// 第七轮（E）：支付渠道配置。
	PayMockSecret  string
	WeChatConfig   pay.WeChatConfig
	AlipayConfig   pay.AlipayConfig
	AppleIAPConfig pay.AppleIAPConfig
	EnableWeChat   bool
	EnableAlipay   bool
	EnableAppleIAP bool
}

// Server billing-svc 工厂结果。
type Server struct {
	HTTP    http.Handler
	service *service.BillingService
	Store   store.Store
	cleanup []func()
}

// New 构造。
func New(ctx context.Context, deps Deps) (*Server, error) {
	srv := &Server{}
	var st store.Store
	if deps.PG != nil {
		if deps.MigrationsDir != "" {
			if err := db.Apply(ctx, deps.PG, deps.MigrationsDir); err != nil {
				return nil, err
			}
		}
		st = store.NewPgStore(deps.PG)
		if deps.Bus != nil {
			relay := db.NewRelay(deps.PG, &busPublisher{bus: deps.Bus}, db.DefaultRelayConfig())
			relay.Start(ctx)
			srv.cleanup = append(srv.cleanup, relay.Stop)
		}
	}
	srv.Store = st

	svc := service.New(st)
	srv.service = svc

	// 渠道注册（mock 永远启用；其它根据 flag 决定）。
	reg := pay.NewRegistry()
	reg.Register(pay.NewMockChannel(pay.MockConfig{Secret: deps.PayMockSecret}))
	if deps.EnableWeChat {
		ch, _ := pay.NewWeChatChannel(deps.WeChatConfig)
		reg.Register(ch)
	}
	if deps.EnableAlipay {
		ch, _ := pay.NewAlipayChannel(deps.AlipayConfig)
		reg.Register(ch)
	}
	if deps.EnableAppleIAP {
		ch, _ := pay.NewAppleIAPChannel(deps.AppleIAPConfig)
		reg.Register(ch)
	}

	probes := observability.Probes{}
	if deps.PG != nil {
		probes["pg"] = observability.PgProbe(deps.PG)
	}
	credReadiness := pay.CheckAllCredentials(deps.WeChatConfig, deps.AlipayConfig, deps.AppleIAPConfig)
	srv.HTTP = transport.NewWithDeps(svc, transport.HandlerDeps{PayRegistry: reg, CredentialReadiness: credReadiness}, probes)
	return srv, nil
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

// busPublisher 把 db.OutboxRow 适配到 eventbus.Bus（与原 main.go 同等）。
type busPublisher struct{ bus eventbus.Bus }

func (p *busPublisher) Publish(ctx context.Context, row db.OutboxRow) error {
	return p.bus.Publish(ctx, eventbus.Envelope{
		Topic:   eventbus.Topic(row.Topic),
		Key:     row.PartitionKey,
		Headers: row.Headers,
		Payload: row.Payload,
	})
}
