// billing-svc 二进制入口（薄壳）：构造 Deps，交给 internal/server 工厂。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/services/billing-svc/server"
)

func main() {
	listen := envOr("YUNMAO_BILLING_LISTEN", ":8104")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := server.Deps{}
	if dbURL := os.Getenv("YUNMAO_DB_URL"); dbURL != "" {
		pool, err := db.Open(ctx, db.Config{URL: dbURL})
		if err != nil {
			log.Fatalf("db.Open: %v", err)
		}
		defer pool.Close()
		deps.PG = pool
		deps.MigrationsDir = envOr("YUNMAO_MIGRATIONS_DIR", "../../migrations")

		if mode := strings.ToLower(envOr("YUNMAO_EVENT_BUS", "memory")); mode == "kafka" {
			b, err := eventbus.Open(eventbus.Config{
				Backend:  eventbus.BackendKafka,
				Brokers:  splitCSV(envOr("YUNMAO_KAFKA_BROKERS", "localhost:9092")),
				ClientID: "billing-svc",
			})
			if err != nil {
				log.Fatalf("kafka: %v", err)
			}
			defer func() { _ = b.Close() }()
			deps.Bus = b
		}
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

	log.Printf("billing-svc listening on %s (db=%t, kafka=%t)", listen, deps.PG != nil, deps.Bus != nil)
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
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
