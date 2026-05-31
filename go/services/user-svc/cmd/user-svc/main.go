// user-svc 二进制入口（薄壳）：构造 Deps，交给 internal/server 工厂。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/db"

	"yunmao.live/services/user-svc/server"
)

func main() {
	listen := envOr("YUNMAO_USER_LISTEN", ":8101")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alg := envOr("YUNMAO_JWT_ALG", "RS256")
	var (
		kp  authjwt.KeyProvider
		err error
	)
	switch alg {
	case "RS256":
		kp, err = authjwt.LoadOrCreateRSKeyProvider("user-svc", "YUNMAO_JWT_RS_KID", "YUNMAO_JWT_RS_PRIVATE_KEY_PATH")
	case "HS256":
		// ADR-0019 第七轮：HS256 路径已删除；快速失败避免老配置悄悄上线。
		log.Fatalf("authjwt: HS256 alg removed (ADR-0019). Use YUNMAO_JWT_ALG=RS256 and YUNMAO_JWT_RS_PRIVATE_KEY_PATH/KMS.")
	default:
		log.Fatalf("unsupported YUNMAO_JWT_ALG=%s (RS256 only since ADR-0019)", alg)
	}
	if err != nil {
		log.Fatalf("authjwt provider: %v", err)
	}

	deps := server.Deps{
		KeyProvider: kp,
		TokenTTL:    24 * time.Hour,
		Audience:    envOr("YUNMAO_JWT_AUDIENCE", "yunmao.gateway"),
	}
	if dbURL := os.Getenv("YUNMAO_DB_URL"); dbURL != "" {
		pool, err := db.Open(ctx, db.Config{URL: dbURL})
		if err != nil {
			log.Fatalf("db.Open: %v", err)
		}
		defer pool.Close()
		deps.PG = pool
		deps.MigrationsDir = os.Getenv("YUNMAO_MIGRATIONS_DIR")
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

	log.Printf("user-svc listening on %s (alg=%s, db=%t)", listen, alg, deps.PG != nil)
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
