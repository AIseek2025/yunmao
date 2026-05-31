// Package server 把 feeding-svc 的 wiring（cache/bus/PG/outbox/timeout worker）
// 抽成可被 import 的工厂，供 cmd/feeding-svc/main.go 与 e2e 测试共享。
package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"yunmao.live/pkg/yunmao/cache"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"
	"yunmao.live/pkg/yunmao/observability"

	feedingpb "yunmao.live/proto/feeding/v1"

	"yunmao.live/services/feeding-svc/publisher"
	"yunmao.live/services/feeding-svc/internal/service"
	"yunmao.live/services/feeding-svc/internal/store"
	"yunmao.live/services/feeding-svc/internal/transport"
)

// Deps 注入 feeding-svc 工厂依赖。
type Deps struct {
	// PG 可选；非空时启用 outbox 模式。
	PG            *pgxpool.Pool
	MigrationsDir string

	// Cache 必需：Idempotent + Cooldown 使用。
	Cache cache.Store

	// Publisher 必需：内存/HTTP/Kafka 三选一。
	Publisher publisher.EventPublisher
	// Bus 可选：仅 outbox 模式 + Kafka 时非空，用于 relay。
	Bus eventbus.Bus

	// Source 事件 source 名称（默认 feeding-svc@server）。
	Source string
	Region string

	// SafetyStore 投喂安全策略来源；nil 时使用 memory。
	SafetyStore feedingsafety.Store
	// FlagStore feature flag 来源；nil 时使用 memory 默认集。
	FlagStore featureflags.Store

	// 自动启动后台 worker（timeout 补偿）。
	StartTimeoutWorker bool
	TimeoutInterval    time.Duration

	// SeedRoom 用于 e2e 注入测试房间（避免依赖 room-svc 全链路）。
	SeedRooms []SeedRoom

	// BillingBaseURL 非空时启用 HTTPBilling（默认 NoopBilling）。
	BillingBaseURL string
	// BillingRequired feature flag：false 时 billing 调用失败仍允许投喂（fallback Noop）。
	BillingRequired bool
}

// SeedRoom 测试用注入房间元数据；与 service.Room 字段对齐但暴露为公共类型。
type SeedRoom struct {
	ID           string
	CatID        string
	DeviceID     string
	FeedingOpen  bool
	NoFeedWindow bool
}

// Server feeding-svc 工厂结果。
type Server struct {
	HTTP    http.Handler
	GRPC    *grpc.Server
	service *service.FeedingService
	cleanup []func()
}

// IsOutboxMode 暴露给上层观察（CLI 启动日志）。
func (s *Server) IsOutboxMode() bool {
	if s.service == nil {
		return false
	}
	return s.service.IsOutboxMode()
}

// New 构造。
func New(ctx context.Context, deps Deps) (*Server, error) {
	if deps.Cache == nil {
		return nil, errors.New("feeding-svc: Cache required")
	}
	if deps.Publisher == nil {
		return nil, errors.New("feeding-svc: Publisher required")
	}

	srv := &Server{}

	if deps.PG != nil && deps.MigrationsDir != "" {
		if err := db.Apply(ctx, deps.PG, deps.MigrationsDir); err != nil {
			return nil, err
		}
	}

	if deps.SafetyStore == nil {
		if deps.PG != nil {
			deps.SafetyStore = feedingsafety.NewPgStore(deps.PG, 0)
		} else {
			deps.SafetyStore = feedingsafety.NewMemoryStore()
		}
	}
	mgr := feedingsafety.NewManager(deps.SafetyStore)

	svc := service.New(deps.Publisher, deps.Cache, mgr)
	if deps.Source == "" {
		deps.Source = "feeding-svc@server"
	}
	svc.SetSource(deps.Source)
	if deps.Region != "" {
		svc.SetRegion(deps.Region)
	}
	for _, r := range deps.SeedRooms {
		svc.RegisterRoom(service.Room{
			ID:           r.ID,
			CatID:        r.CatID,
			DeviceID:     r.DeviceID,
			FeedingOpen:  r.FeedingOpen,
			NoFeedWindow: r.NoFeedWindow,
		})
	}

	if deps.FlagStore == nil {
		if deps.PG != nil {
			deps.FlagStore = featureflags.NewPgStore(deps.PG)
		} else {
			deps.FlagStore = featureflags.NewMemoryStore(
				featureflags.Flag{Name: "feeding.allow_new_rooms", Enabled: true, Value: map[string]any{}},
				featureflags.Flag{Name: "feeding.region_qps_limit", Enabled: true, Value: map[string]any{"per_region": 200.0}},
				featureflags.Flag{Name: "feeding.device_maintenance", Enabled: false, Value: map[string]any{"device_ids": []any{}}},
				featureflags.Flag{Name: "feeding.timeout_seconds", Enabled: true, Value: map[string]any{"seconds": 30.0}},
			)
		}
	}
	flagMgr := featureflags.NewManager(featureflags.Config{Store: deps.FlagStore, RefreshEvery: 10 * time.Second})
	flagMgr.Start(ctx)
	svc.SetFlags(flagMgr)

	if deps.StartTimeoutWorker {
		interval := deps.TimeoutInterval
		if interval == 0 {
			interval = 5 * time.Second
		}
		svc.StartTimeoutWorker(ctx, interval, func() time.Duration {
			return time.Duration(flagMgr.Int("feeding.timeout_seconds", "seconds", 30)) * time.Second
		})
	}

	if deps.PG != nil {
		pgStore := store.NewPgStore(deps.PG)
		listener := &service.OutboxListener{Store: pgStore, Source: deps.Source}
		svc.AddEventListener(listener.Handle)

		if deps.Bus != nil {
			svc.SetOutboxMode(true)
			relay := db.NewRelay(deps.PG, &store.OutboxKafkaPublisher{Bus: deps.Bus}, db.DefaultRelayConfig())
			relay.Start(ctx)
			srv.cleanup = append(srv.cleanup, relay.Stop)
		}
	}

	if deps.BillingBaseURL != "" {
		hb := service.NewHTTPBilling(deps.BillingBaseURL)
		hb.FallbackOnError = !deps.BillingRequired
		svc.SetBilling(hb)
	}

	srv.service = svc
	probes := observability.Probes{}
	if deps.PG != nil {
		probes["pg"] = observability.PgProbe(deps.PG)
	}
	srv.HTTP = transport.New(svc, probes)

	grpcSrv := grpc.NewServer()
	feedingpb.RegisterFeedingServiceServer(grpcSrv, transport.NewGrpcServer(svc))
	srv.GRPC = grpcSrv
	return srv, nil
}

// Close 释放资源。
func (s *Server) Close() {
	for _, fn := range s.cleanup {
		fn()
	}
	if s.GRPC != nil {
		s.GRPC.GracefulStop()
	}
}

// ListenAndServe 同时启动 HTTP + gRPC。ctx 取消时优雅停机。
func (s *Server) ListenAndServe(ctx context.Context, httpAddr, grpcAddr string) error {
	hs := &http.Server{Addr: httpAddr, Handler: s.HTTP, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 2)

	go func() {
		err := hs.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	if grpcAddr != "" {
		lis, err := net.Listen("tcp", grpcAddr)
		if err != nil {
			return err
		}
		go func() { errCh <- s.GRPC.Serve(lis) }()
	}

	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shCtx)
		s.GRPC.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}
