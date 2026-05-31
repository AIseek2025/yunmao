// Package db: embedded.go 提供单一入口 `Apply`，让所有 Go 服务在启动时统一加载
// 同一份 migrations 目录（go/migrations/）。
//
// 为何不直接 embed：
//
//   - sqlc 生成的 Go 代码会带自己的 //go:embed schema，二者要保持可独立 evolve。
//   - migrations 目录历史上由顶层 Makefile + docker-compose 统一执行；本函数让 Go 服务
//     在 dev 模式可直接从源码树读取，并落进 schema_migrations 跟踪。
//
// 用法：
//
//	pool, _ := db.Open(ctx, db.Config{URL: os.Getenv("YUNMAO_DB_URL")})
//	if err := db.Apply(ctx, pool, "../../migrations"); err != nil { ... }
package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Apply 从一个文件系统目录加载并执行迁移。空字符串则跳过。
//
// 在容器里运行时，传 `/app/migrations`；在 dev 里传相对源码树的路径。
func Apply(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	if dir == "" {
		return nil
	}
	migs, err := LoadMigrationsFS(os.DirFS(dir), ".")
	if err != nil {
		return fmt.Errorf("db.Apply: load from %s: %w", dir, err)
	}
	return Migrate(ctx, pool, migs)
}
