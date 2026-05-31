// Package store chat-svc 持久化层（内存 / PG）。
package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"yunmao.live/services/chat-svc/internal/service"
)

// NewMemory 进程内实现。
func NewMemory() service.Store { return &memStore{m: map[string]service.Message{}} }

type memStore struct {
	mu sync.Mutex
	m  map[string]service.Message
}

func (s *memStore) Insert(_ context.Context, m service.Message) (*service.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[m.ID]; ok {
		return nil, errors.New("duplicate id")
	}
	cp := m
	s.m[m.ID] = cp
	return &cp, nil
}

func (s *memStore) Get(_ context.Context, id string) (*service.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.m[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	cp := m
	return &cp, nil
}

func (s *memStore) Moderate(_ context.Context, id, status, reason string) (*service.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.m[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	m.ModerationStatus = status
	m.ModerationReason = reason
	s.m[id] = m
	cp := m
	return &cp, nil
}

func (s *memStore) List(_ context.Context, roomID string, limit int) ([]service.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]service.Message, 0, limit)
	for _, m := range s.m {
		if m.RoomID == roomID && (m.ModerationStatus == "published" || m.ModerationStatus == "flagged") {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---- PG ----

// NewPg PG-backed 实现。
func NewPg(pool *pgxpool.Pool) service.Store { return &pgStore{pool: pool} }

type pgStore struct {
	pool *pgxpool.Pool
}

func (p *pgStore) Insert(ctx context.Context, m service.Message) (*service.Message, error) {
	emojis := []byte("[]")
	if len(m.Emojis) > 0 {
		emojis = []byte(`["` + joinSafe(m.Emojis) + `"]`)
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO chat_messages
		(id, user_id, room_id, body, emojis, moderation_status, moderation_reason,
		 client_msg_id, region_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,NULLIF($7,''),NULLIF($8,''),'global',$9,$9)
	`, m.ID, m.UserID, m.RoomID, m.Body, string(emojis),
		m.ModerationStatus, m.ModerationReason, m.ClientMsgID, m.CreatedAt)
	if err != nil {
		return nil, err
	}
	cp := m
	return &cp, nil
}

func (p *pgStore) Get(ctx context.Context, id string) (*service.Message, error) {
	m := &service.Message{}
	var reason, clientMsg *string
	var updatedAt *time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT id, user_id, room_id, body, moderation_status, moderation_reason,
		       client_msg_id, created_at, updated_at
		FROM chat_messages WHERE id=$1
	`, id).Scan(
		&m.ID, &m.UserID, &m.RoomID, &m.Body, &m.ModerationStatus,
		&reason, &clientMsg, &m.CreatedAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if reason != nil {
		m.ModerationReason = *reason
	}
	if clientMsg != nil {
		m.ClientMsgID = *clientMsg
	}
	return m, nil
}

func (p *pgStore) Moderate(ctx context.Context, id, status, reason string) (*service.Message, error) {
	_, err := p.pool.Exec(ctx, `
		UPDATE chat_messages SET moderation_status=$1, moderation_reason=NULLIF($2,''), updated_at=NOW()
		WHERE id=$3`, status, reason, id)
	if err != nil {
		return nil, err
	}
	return p.Get(ctx, id)
}

func (p *pgStore) List(ctx context.Context, roomID string, limit int) ([]service.Message, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, user_id, room_id, body, moderation_status, COALESCE(moderation_reason,''),
		       COALESCE(client_msg_id,''), created_at
		FROM chat_messages
		WHERE room_id=$1 AND moderation_status IN ('published','flagged')
		ORDER BY created_at DESC LIMIT $2
	`, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]service.Message, 0, limit)
	for rows.Next() {
		var m service.Message
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.RoomID, &m.Body, &m.ModerationStatus,
			&m.ModerationReason, &m.ClientMsgID, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func joinSafe(in []string) string {
	out := ""
	for i, s := range in {
		// 极简 escape：只去掉双引号，避免 SQL/JSON 注入。
		clean := ""
		for _, r := range s {
			if r != '"' && r != '\\' && r >= 0x20 {
				clean += string(r)
			}
		}
		if i > 0 {
			out += `","`
		}
		out += clean
	}
	return out
}
