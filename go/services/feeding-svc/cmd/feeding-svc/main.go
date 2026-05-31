// feeding-svc 二进制入口（薄壳）：解析环境变量、构造 Deps、交给 internal/server 工厂。
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"yunmao.live/pkg/yunmao/cache"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"

	"yunmao.live/services/feeding-svc/publisher"
	"yunmao.live/services/feeding-svc/server"
)

func main() {
	listen := envOr("YUNMAO_FEEDING_LISTEN", ":8201")
	grpcAddr := envOr("YUNMAO_FEEDING_GRPC_LISTEN", ":9201")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	storeCfg := cache.Config{
		Backend: cache.Backend(envOr("YUNMAO_CACHE_BACKEND", string(cache.BackendMemory))),
		URL:     os.Getenv("YUNMAO_REDIS_URL"),
	}
	cacheStore, err := cache.Open(ctx, storeCfg)
	if err != nil {
		log.Fatalf("cache.Open: %v", err)
	}
	defer cacheStore.Close()

	pub, busHandle, closer, err := buildPublisher()
	if err != nil {
		log.Fatalf("buildPublisher: %v", err)
	}
	if closer != nil {
		defer closer()
	}

	deps := server.Deps{
		Cache:              cacheStore,
		Publisher:          pub,
		Bus:                busHandle,
		Source:             envOr("YUNMAO_FEEDING_SOURCE", "feeding-svc@kafka"),
		Region:             envOr("YUNMAO_REGION", "global"),
		StartTimeoutWorker: true,
	}

	if dbURL := os.Getenv("YUNMAO_DB_URL"); dbURL != "" {
		pool, err := db.Open(ctx, db.Config{URL: dbURL})
		if err != nil {
			log.Fatalf("db.Open: %v", err)
		}
		defer pool.Close()
		deps.PG = pool
		deps.MigrationsDir = envOr("YUNMAO_MIGRATIONS_DIR", "../../migrations")
	}

	srv, err := server.New(ctx, deps)
	if err != nil {
		log.Fatalf("server.New: %v", err)
	}
	defer srv.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		cancel()
	}()

	log.Printf("feeding-svc listening on %s (grpc=%s, event_bus=%s, cache=%s, db=%t, outbox=%t)",
		listen, grpcAddr, envOr("YUNMAO_EVENT_BUS", "memory"), storeCfg.Backend,
		deps.PG != nil, srv.IsOutboxMode())
	if err := srv.ListenAndServe(ctx, listen, grpcAddr); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func buildPublisher() (publisher.EventPublisher, eventbus.Bus, func(), error) {
	mode := strings.ToLower(envOr("YUNMAO_EVENT_BUS", "memory"))
	switch mode {
	case "memory":
		bus := eventbus.NewMemoryBus()
		return publisher.NewKafka(bus, "feeding-svc@dev"), bus, func() { _ = bus.Close() }, nil
	case "http":
		deviceEdge := envOr("YUNMAO_DEVICE_EDGE_URL", "http://127.0.0.1:8091")
		gateway := envOr("YUNMAO_GATEWAY_URL", "http://127.0.0.1:8090")
		return publisher.NewHTTP(deviceEdge, gateway), nil, func() {}, nil
	case "kafka":
		brokers := splitCSV(envOr("YUNMAO_KAFKA_BROKERS", "localhost:9092"))
		bus, err := eventbus.Open(eventbus.Config{
			Backend:  eventbus.BackendKafka,
			Brokers:  brokers,
			ClientID: "feeding-svc",
		})
		if err != nil {
			return nil, nil, nil, err
		}
		return publisher.NewKafka(bus, "feeding-svc@kafka"), bus, func() { _ = bus.Close() }, nil
	default:
		return nil, nil, nil, errors.New("YUNMAO_EVENT_BUS must be memory|http|kafka")
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
