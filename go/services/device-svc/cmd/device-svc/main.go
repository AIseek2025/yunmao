// device-svc 二进制入口（薄壳）：构造 Deps，交给 internal/server 工厂。
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/mqttx"

	"yunmao.live/services/device-svc/server"
)

func main() {
	listen := envOr("YUNMAO_DEVICE_LISTEN", ":8103")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alg := envOr("YUNMAO_DEVICE_JWT_ALG", "RS256")
	var (
		kp  authjwt.KeyProvider
		err error
	)
	switch alg {
	case "RS256":
		kp, err = authjwt.LoadOrCreateRSKeyProvider("device-svc",
			"YUNMAO_DEVICE_JWT_RS_KID", "YUNMAO_DEVICE_JWT_RS_PRIVATE_KEY_PATH")
	case "HS256":
		log.Fatalf("authjwt: HS256 alg removed (ADR-0019). Use YUNMAO_DEVICE_JWT_ALG=RS256.")
	default:
		log.Fatalf("unsupported YUNMAO_DEVICE_JWT_ALG=%s (RS256 only since ADR-0019)", alg)
	}
	if err != nil {
		log.Fatalf("device-svc keyprovider: %v", err)
	}

	credTTL, err := time.ParseDuration(envOr("YUNMAO_DEVICE_JWT_TTL", "12h"))
	if err != nil {
		credTTL = 12 * time.Hour
	}

	deps := server.Deps{
		KeyProvider:       kp,
		MqttCredentialTTL: credTTL,
		RegionID:          envOr("YUNMAO_REGION", "global"),
		FeedingGRPCAddr:   envOr("YUNMAO_FEEDING_GRPC_ADDR", "127.0.0.1:9201"),
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

	if mqttBrokers := os.Getenv("YUNMAO_MQTT_BROKERS"); mqttBrokers != "" {
		bus, cli, closer, err := buildBridge(ctx, mqttBrokers)
		if err != nil {
			log.Printf("device-svc bridge disabled: %v", err)
		} else {
			defer closer()
			deps.Bus = bus
			deps.MQTT = cli
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

	log.Printf("device-svc listening on %s (mqtt=%t db=%t alg=%s)",
		listen, deps.MQTT != nil, deps.PG != nil, alg)
	if err := srv.ListenAndServe(ctx, listen); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func buildBridge(ctx context.Context, mqttBrokers string) (eventbus.Bus, mqttx.Client, func(), error) {
	mode := strings.ToLower(envOr("YUNMAO_EVENT_BUS", "memory"))
	var bus eventbus.Bus
	switch mode {
	case "memory":
		bus = eventbus.NewMemoryBus()
	case "kafka":
		var err error
		bus, err = eventbus.Open(eventbus.Config{
			Backend:  eventbus.BackendKafka,
			Brokers:  splitCSV(envOr("YUNMAO_KAFKA_BROKERS", "localhost:9092")),
			ClientID: "device-svc",
		})
		if err != nil {
			return nil, nil, nil, err
		}
	default:
		return nil, nil, nil, errors.New("YUNMAO_EVENT_BUS must be memory|kafka")
	}
	cli, err := mqttx.Dial(ctx, mqttx.Config{
		Brokers:        splitCSV(mqttBrokers),
		ClientID:       envOr("YUNMAO_MQTT_CLIENT_ID", "device-svc"),
		Username:       os.Getenv("YUNMAO_MQTT_USERNAME"),
		Password:       os.Getenv("YUNMAO_MQTT_PASSWORD"),
		CleanSession:   true,
		KeepAlive:      30 * time.Second,
		ConnectTimeout: 5 * time.Second,
		AutoReconnect:  true,
	})
	if err != nil {
		_ = bus.Close()
		return nil, nil, nil, err
	}
	closer := func() {
		_ = cli.Disconnect(100)
		_ = bus.Close()
	}
	return bus, cli, closer, nil
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

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
