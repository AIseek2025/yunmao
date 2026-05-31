// Package server 把 device-svc 的 wiring（DB/bridge/JWT）抽为工厂。
package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/mqttx"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/device-svc/internal/bridge"
	"yunmao.live/services/device-svc/internal/service"
	"yunmao.live/services/device-svc/internal/store"
	"yunmao.live/services/device-svc/internal/transport"
)

// Deps 注入 device-svc 工厂依赖。
type Deps struct {
	PG               *pgxpool.Pool
	MigrationsDir    string
	KeyProvider      authjwt.KeyProvider
	MqttCredentialTTL time.Duration
	RegionID          string

	// Bus + MQTT 同时非空时启动双向 bridge。
	Bus  eventbus.Bus
	MQTT mqttx.Client

	FeedingGRPCAddr string
}

// Server device-svc 工厂结果。
type Server struct {
	HTTP    http.Handler
	service *service.DeviceService
	Bridge  *bridge.Bridge
	cleanup []func()
}

// New 构造。
func New(ctx context.Context, deps Deps) (*Server, error) {
	if deps.KeyProvider == nil {
		return nil, errors.New("device-svc: KeyProvider required")
	}
	if deps.MqttCredentialTTL == 0 {
		deps.MqttCredentialTTL = 12 * time.Hour
	}
	if deps.RegionID == "" {
		deps.RegionID = "global"
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

	svc := service.New(service.Config{
		Store:             st,
		KeyProvider:       deps.KeyProvider,
		MqttCredentialTTL: deps.MqttCredentialTTL,
		RegionID:          deps.RegionID,
	})

	probes := observability.Probes{}
	if deps.PG != nil {
		probes["pg"] = observability.PgProbe(deps.PG)
	}
	if deps.MQTT != nil {
		probes["mqtt"] = observability.MqttProbe(deps.MQTT.IsConnected)
	}
	probes["keys"] = observability.KeysProbe(func() (string, error) {
		sk, err := deps.KeyProvider.Active()
		if err != nil {
			return "", err
		}
		return string(sk.Alg), nil
	})
	srv := &Server{
		service: svc,
		HTTP:    transport.New(svc, transport.Config{FeedingGRPCAddr: deps.FeedingGRPCAddr}, probes),
	}

	if deps.Bus != nil && deps.MQTT != nil {
		b := bridge.New(deps.Bus, deps.MQTT, bridge.DefaultConfig())
		b.SetOnAck(func(ctx context.Context, ack bridge.FeedAckPayload) {
			svc.MarkOnline(ctx, ack.DeviceID, ack.RemainingFoodGrams)
		})
		b.SetOnHeartbeat(func(ctx context.Context, deviceID string, at time.Time) {
			svc.HeartbeatAt(ctx, deviceID, at)
		})
		if err := b.Start(ctx); err != nil {
			return nil, err
		}
		srv.Bridge = b
	}

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
