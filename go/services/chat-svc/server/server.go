// Package server 把 chat-svc wiring 封装为工厂；cmd/main.go 与 e2e 测试共享。
package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yunmao.live/pkg/yunmao/cache"
	"yunmao.live/pkg/yunmao/cloudevents"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/chat-svc/internal/moderation"
	"yunmao.live/services/chat-svc/internal/service"
	"yunmao.live/services/chat-svc/internal/store"
	"yunmao.live/services/chat-svc/internal/transport"
)

// Deps chat-svc 工厂依赖。
type Deps struct {
	PG            *pgxpool.Pool
	MigrationsDir string
	Cache         cache.Store
	Bus           eventbus.Bus
	Source        string

	// 第七轮（F）：moderation provider 配置。
	//   ModerationProvider ∈ {"", "local", "aliyun_green"}；空 = local。
	//   AliyunGreen* 仅在 ModerationProvider="aliyun_green" 时使用；
	//   凭据缺失时退回 local + 打 metrics。
	ModerationProvider     string
	AliyunGreenAccessKey   string
	AliyunGreenAccessSecret string
	AliyunGreenRegion      string
	AliyunGreenMockMode    bool
}

// Server 工厂结果。
type Server struct {
	HTTP    http.Handler
	cleanup []func()
}

// New 构造。
func New(ctx context.Context, deps Deps) (*Server, error) {
	if deps.Cache == nil {
		deps.Cache = cache.NewMemoryStore()
	}
	var s service.Store
	if deps.PG != nil {
		if deps.MigrationsDir != "" {
			if err := db.Apply(ctx, deps.PG, deps.MigrationsDir); err != nil {
				return nil, err
			}
		}
		s = store.NewPg(deps.PG)
	} else {
		s = store.NewMemory()
	}

	var pub service.Publisher
	if deps.Bus != nil {
		pub = &busPublisher{bus: deps.Bus}
	}

	// 第七轮（F）：构建 moderation Manager（primary + local fallback）。
	local := moderation.NewLocalProvider(nil)
	var primary moderation.Provider = local
	switch deps.ModerationProvider {
	case "aliyun_green":
		p, err := moderation.NewAliyunGreenProvider(moderation.AliyunGreenConfig{
			AccessKey:    deps.AliyunGreenAccessKey,
			AccessSecret: deps.AliyunGreenAccessSecret,
			Region:       deps.AliyunGreenRegion,
			MockMode:     deps.AliyunGreenMockMode || deps.AliyunGreenAccessKey == "",
		})
		if err == nil {
			primary = p
		}
	}
	mgr := moderation.NewManager(primary, local, 250*time.Millisecond)
	filter := service.NewModerationManagerFilter(mgr, 250*time.Millisecond)

	svc := service.New(service.Config{
		Store:          s,
		Publisher:      pub,
		Filter:         filter,
		RateLimitStore: deps.Cache,
		Source:         deps.Source,
	})

	probes := observability.Probes{}
	if deps.PG != nil {
		probes["pg"] = observability.PgProbe(deps.PG)
	}
	return &Server{HTTP: transport.New(svc, probes)}, nil
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

type busPublisher struct {
	bus eventbus.Bus
}

func (b *busPublisher) Publish(ctx context.Context, topic eventbus.Topic, key string, evt cloudevents.Event[any]) error {
	env, err := eventbus.NewEnvelope(topic, key, evt)
	if err != nil {
		return err
	}
	return b.bus.Publish(ctx, env)
}
