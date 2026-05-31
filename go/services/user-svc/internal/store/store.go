// Package store 实现 user-svc 的持久化层。
//
// 提供：
//   - MemoryStore：兼容内存路径（默认）。
//   - PgStore：基于 pgxpool 的 PostgreSQL 持久化。
//
// 设计目标是与 service.UserService 的 in-memory 实现 1:1 对齐：
// service 只持有 Store 接口，由 cmd/main.go 决定具体实现。
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	yerr "yunmao.live/pkg/yunmao/errors"
)

// User 持久化层的用户 DTO（与 service.User 同形）。
type User struct {
	ID        string
	Nickname  string
	PhoneHash string
	Role      string
	CreatedAt time.Time
}

// LoginHistoryEntry 登录历史记录。
// 与 migrations/0003_third_iteration.sql 中 login_history 表对齐。
type LoginHistoryEntry struct {
	UserID    string
	Channel   string // "sms" | "dev" | "wechat" | "apple" ...
	IP        string // 远端 IP（可空）
	UserAgent string
	JwtKid    string
	At        time.Time
}

// Store 是 user-svc 用到的持久化接口。
type Store interface {
	GetByID(ctx context.Context, id string) (*User, error)
	GetByPhoneHash(ctx context.Context, phoneHash string) (*User, error)
	Upsert(ctx context.Context, u *User) error
	AppendLogin(ctx context.Context, e LoginHistoryEntry) error
}

// ----- MemoryStore -----

// NewMemoryStore 返回内存存储；用于 PoC / 单元测试。
func NewMemoryStore() Store {
	return &memoryStore{
		byID: map[string]*User{},
		byPh: map[string]*User{},
	}
}

type memoryStore struct {
	byID map[string]*User
	byPh map[string]*User
}

func (m *memoryStore) GetByID(_ context.Context, id string) (*User, error) {
	u, ok := m.byID[id]
	if !ok {
		return nil, yerr.New(yerr.UserNotFound, "user not found")
	}
	cp := *u
	return &cp, nil
}

func (m *memoryStore) GetByPhoneHash(_ context.Context, ph string) (*User, error) {
	u, ok := m.byPh[ph]
	if !ok {
		return nil, yerr.New(yerr.UserNotFound, "user not found")
	}
	cp := *u
	return &cp, nil
}

func (m *memoryStore) Upsert(_ context.Context, u *User) error {
	if u == nil || u.ID == "" {
		return errors.New("user.id required")
	}
	cp := *u
	m.byID[u.ID] = &cp
	if u.PhoneHash != "" {
		m.byPh[u.PhoneHash] = &cp
	}
	return nil
}

func (m *memoryStore) AppendLogin(_ context.Context, _ LoginHistoryEntry) error {
	return nil
}

// ----- PgStore -----

// NewPgStore 返回基于 pgxpool 的持久化实现。
func NewPgStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

type pgStore struct {
	pool *pgxpool.Pool
}

// 注意：migration 0001 中 users.phone_hash 是 NOT NULL UNIQUE。
// DevLogin 路径下若没有手机号，PhoneHash 字段会留空——这会触发 UNIQUE
// 冲突，因此持久层把空字符串映射为 NULLIF→COALESCE 路径，让空 PhoneHash
// 走 random 占位（user id 本身作为 fallback），保证多次 DevLogin 不会
// 因 phone_hash 冲突而插入失败。
const sqlUpsertUser = `
INSERT INTO users (id, nickname, phone_hash, role, created_at, updated_at)
VALUES ($1, $2, COALESCE(NULLIF($3, ''), $1), $4, $5, $5)
ON CONFLICT (id) DO UPDATE SET
  nickname  = EXCLUDED.nickname,
  phone_hash = COALESCE(NULLIF(EXCLUDED.phone_hash, ''), users.phone_hash),
  role      = EXCLUDED.role,
  updated_at = NOW()`

const sqlSelectByID = `SELECT id, nickname, COALESCE(phone_hash, ''), role, created_at FROM users WHERE id = $1`
const sqlSelectByPhone = `SELECT id, nickname, COALESCE(phone_hash, ''), role, created_at FROM users WHERE phone_hash = $1 LIMIT 1`
const sqlInsertLogin = `
INSERT INTO login_history (user_id, channel, ip, user_agent, jwt_kid, created_at)
VALUES ($1, $2, NULLIF($3, '')::inet, NULLIF($4, ''), NULLIF($5, ''), $6)`

func (p *pgStore) GetByID(ctx context.Context, id string) (*User, error) {
	row := p.pool.QueryRow(ctx, sqlSelectByID, id)
	return scanUser(row)
}

func (p *pgStore) GetByPhoneHash(ctx context.Context, ph string) (*User, error) {
	row := p.pool.QueryRow(ctx, sqlSelectByPhone, ph)
	return scanUser(row)
}

func (p *pgStore) Upsert(ctx context.Context, u *User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	if u.Role == "" {
		u.Role = "user"
	}
	_, err := p.pool.Exec(ctx, sqlUpsertUser, u.ID, u.Nickname, u.PhoneHash, u.Role, u.CreatedAt)
	return err
}

func (p *pgStore) AppendLogin(ctx context.Context, e LoginHistoryEntry) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if e.Channel == "" {
		e.Channel = "sms"
	}
	_, err := p.pool.Exec(ctx, sqlInsertLogin, e.UserID, e.Channel, e.IP, e.UserAgent, e.JwtKid, e.At)
	return err
}

func scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	if err := row.Scan(&u.ID, &u.Nickname, &u.PhoneHash, &u.Role, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, yerr.New(yerr.UserNotFound, "user not found")
		}
		return nil, err
	}
	return u, nil
}
