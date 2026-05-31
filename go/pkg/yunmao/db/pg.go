// Package db 提供 PostgreSQL 连接池（pgx/v5）+ 内嵌迁移执行器。
//
// 设计目标：
//
//   - 服务启动时，按需 `Open` 一个 `*pgxpool.Pool`，并 `Migrate` 把 SQL 迁移文件按顺序跑掉。
//   - 兼容 sqlc：连接池由本包统一管理，sqlc 生成的 *Queries 直接接 `pool.WithTx(...)`。
//   - 不要求把全部 schema 留在内嵌资源里 —— 通过 `migrations.go` 在每个服务里 `//go:embed`，
//     以便不同服务能挑自己关心的 migrations 子集（admin/feeding/billing 等）。
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config 数据库配置。
type Config struct {
	// URL 形如 postgres://user:pwd@host:5432/db?sslmode=disable
	URL string
	// MaxConns 默认 10。
	MaxConns int32
	// HealthCheckPeriod 默认 30s。
	HealthCheckPeriod time.Duration
}

// Open 创建连接池并 ping 一次。
func Open(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.URL == "" {
		return nil, errors.New("db: URL required")
	}
	pcfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	}
	if cfg.HealthCheckPeriod > 0 {
		pcfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("db: new pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migration 单个迁移。
type Migration struct {
	Name string // e.g. "0001_init.sql"
	SQL  string
}

// LoadMigrationsFS 从一个 fs.FS 加载所有 `*.sql` 迁移；按文件名升序排序。
// 支持 goose 风格的 `-- +goose Up / Down` 标记：只执行 Up 段。
func LoadMigrationsFS(fsys fs.FS, root string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(root, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("db: read %s: %w", e.Name(), err)
		}
		out = append(out, Migration{Name: e.Name(), SQL: extractUp(string(raw))})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// extractUp 在 goose 风格的迁移文件中只保留 `-- +goose Up` 与 `-- +goose Down` 之间内容。
// 没有标记则直接返回整段。
func extractUp(s string) string {
	const upTag = "-- +goose Up"
	const downTag = "-- +goose Down"
	upIdx := strings.Index(s, upTag)
	if upIdx < 0 {
		return s
	}
	rest := s[upIdx+len(upTag):]
	if downIdx := strings.Index(rest, downTag); downIdx >= 0 {
		return rest[:downIdx]
	}
	return rest
}

// Migrate 在事务中按名称顺序执行未应用的迁移；使用 `schema_migrations` 表跟踪。
// 失败会回滚当前事务，前面已完成的迁移仍然保留（每个迁移自身一个事务）。
func Migrate(ctx context.Context, pool *pgxpool.Pool, migs []Migration) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name        TEXT PRIMARY KEY,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}

	applied := make(map[string]bool)
	rows, err := pool.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("db: load applied: %w", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		applied[n] = true
	}
	rows.Close()

	for _, m := range migs {
		if applied[m.Name] {
			continue
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("db: begin %s: %w", m.Name, err)
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("db: exec %s: %w", m.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, m.Name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("db: track %s: %w", m.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("db: commit %s: %w", m.Name, err)
		}
	}
	return nil
}
