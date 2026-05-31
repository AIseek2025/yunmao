// chat-svc 二进制入口。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"yunmao.live/pkg/yunmao/cache"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"

	"yunmao.live/services/chat-svc/server"
)

func main() {
	listen := envOr("YUNMAO_CHAT_LISTEN", ":8204")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cacheStore, err := cache.Open(ctx, cache.Config{
		Backend: cache.Backend(envOr("YUNMAO_CACHE_BACKEND", string(cache.BackendMemory))),
		URL:     os.Getenv("YUNMAO_REDIS_URL"),
	})
	if err != nil {
		log.Fatalf("cache.Open: %v", err)
	}
	defer cacheStore.Close()

	deps := server.Deps{
		Cache:  cacheStore,
		Source: envOr("YUNMAO_CHAT_SOURCE", "chat-svc@server"),
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
	if mode := strings.ToLower(envOr("YUNMAO_EVENT_BUS", "memory")); mode != "" {
		bus, err := eventbus.Open(eventbus.Config{
			Backend:  eventbus.Backend(mode),
			Brokers:  splitCSV(envOr("YUNMAO_KAFKA_BROKERS", "localhost:9092")),
			ClientID: "chat-svc",
		})
		if err != nil {
			log.Printf("eventbus.Open: %v (fallback memory)", err)
			bus = eventbus.NewMemoryBus()
		}
		defer bus.Close()
		deps.Bus = bus
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
	log.Printf("chat-svc listening on %s (db=%t)", listen, deps.PG != nil)
	if err := srv.ListenAndServe(ctx, listen); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
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
