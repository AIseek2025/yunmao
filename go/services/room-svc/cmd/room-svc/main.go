// room-svc 二进制入口（薄壳）：构造 Deps，交给 internal/server 工厂。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/db"

	"yunmao.live/services/room-svc/server"
)

func main() {
	listen := envOr("YUNMAO_ROOM_LISTEN", ":8102")

	alg := envOr("YUNMAO_JWT_ALG", "RS256")
	var (
		kp  authjwt.KeyProvider
		err error
	)
	switch alg {
	case "RS256":
		kp, err = authjwt.LoadOrCreateRSKeyProvider("room-svc", "YUNMAO_JWT_RS_KID", "YUNMAO_JWT_RS_PRIVATE_KEY_PATH")
	case "HS256":
		log.Fatalf("authjwt: HS256 alg removed (ADR-0019). Use YUNMAO_JWT_ALG=RS256.")
	default:
		log.Fatalf("unsupported YUNMAO_JWT_ALG=%s (RS256 only since ADR-0019)", alg)
	}
	if err != nil {
		log.Fatalf("authjwt provider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var verifierKp authjwt.KeyProvider = kp
	if eps := envOr("YUNMAO_JWKS_ENDPOINTS", ""); eps != "" {
		verifierKp = authjwt.NewJWKSClient(splitComma(eps), 5*time.Minute)
	}

	turnHosts := splitComma(envOr("YUNMAO_TURN_HOSTS", ""))
	turnPorts := []int{}
	for _, p := range splitComma(envOr("YUNMAO_TURN_PORTS", "3478")) {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			turnPorts = append(turnPorts, n)
		}
	}
	turnTTL, terr := time.ParseDuration(envOr("YUNMAO_TURN_TTL", "5m"))
	if terr != nil {
		turnTTL = 5 * time.Minute
	}

	deps := server.Deps{
		KeyProvider:       kp,
		VerifierProvider:  verifierKp,
		TokenTTL:          10 * time.Minute,
		Audience:          envOr("YUNMAO_JWT_AUDIENCE", "yunmao.gateway"),
		RegionID:          envOr("YUNMAO_REGION", "global"),
		StreamHMACSecret:  []byte(envOr("YUNMAO_STREAM_HMAC_SECRET", "")),
		AllowGuest:        envOr("YUNMAO_ALLOW_GUEST", "true") == "true",
		TurnPrimarySecret: []byte(os.Getenv("YUNMAO_TURN_SHARED_SECRET")),
		TurnLegacySecret:  []byte(os.Getenv("YUNMAO_TURN_SHARED_SECRET_LEGACY")),
		TurnHosts:         turnHosts,
		TurnPorts:         turnPorts,
		StunUrls:          splitComma(envOr("YUNMAO_STUN_URLS", "stun:stun.l.google.com:19302")),
		TurnTTL:           turnTTL,
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

	log.Printf("room-svc listening on %s (allowGuest=%v alg=%s db=%t)",
		listen, deps.AllowGuest, alg, deps.PG != nil)
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

func splitComma(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
