// admin-svc 二进制入口（薄壳）：仅负责构造 Deps 并交给 internal/server 工厂。
//
// 环境变量：
//
//   - YUNMAO_ADMIN_LISTEN：HTTP 监听地址，默认 :8105
//   - YUNMAO_DB_URL：postgres URL；非空时启用 PG-backed safety + flags
//   - YUNMAO_MIGRATIONS_DIR：迁移目录
//   - YUNMAO_BILLING_URL：billing-svc upstream base URL（可选）
//   - YUNMAO_ROOM_URL：room-svc upstream base URL（可选）
//   - YUNMAO_JWT_RS_PRIVATE_KEY_PATH：RSA PEM 私钥路径；非空时 admin 鉴权全链路激活
//   - YUNMAO_JWT_KID：与私钥匹配的 kid；非空时激活鉴权
//   - YUNMAO_ADMIN_PASSWORD：管理员登录密码；非空时启用 POST /v1/auth/admin/login
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/services/admin-svc/server"
)

func main() {
	listen := envOr("YUNMAO_ADMIN_LISTEN", ":8105")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := server.Deps{
		BillingBaseURL: os.Getenv("YUNMAO_BILLING_URL"),
		RoomBaseURL:    os.Getenv("YUNMAO_ROOM_URL"),
		AdminPassword:  os.Getenv("YUNMAO_ADMIN_PASSWORD"),
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

	if kid := os.Getenv("YUNMAO_JWT_KID"); kid != "" {
		kp, err := authjwt.LoadOrCreateRSKeyProvider(
			"admin-svc", "YUNMAO_JWT_KID", "YUNMAO_JWT_RS_PRIVATE_KEY_PATH",
		)
		if err != nil {
			log.Fatalf("authjwt.LoadOrCreateRSKeyProvider: %v", err)
		}
		v, err := authjwt.NewVerifierFromProvider(kp)
		if err != nil {
			log.Fatalf("authjwt.NewVerifierFromProvider: %v", err)
		}
		deps.Verifier = v

		s, err := authjwt.NewSignerFromProvider(kp, "yunmao.admin-svc")
		if err != nil {
			log.Fatalf("authjwt.NewSignerFromProvider: %v", err)
		}
		deps.Signer = s
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

	log.Printf("admin-svc listening on %s (db=%t, auth=%t, login=%t, billing=%t, rooms=%t)",
		listen, deps.PG != nil, deps.Verifier != nil,
		deps.Signer != nil && deps.AdminPassword != "",
		deps.BillingBaseURL != "", deps.RoomBaseURL != "")
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
