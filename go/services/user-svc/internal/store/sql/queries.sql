-- sqlc 查询：user-svc 落库 SQL（与 internal/store/store.go 中 sql* 常量一致）。

-- name: UpsertUser :exec
INSERT INTO users (id, nickname, phone_hash, role, created_at, updated_at)
VALUES ($1, $2, COALESCE(NULLIF($3, ''), $1), $4, $5, $5)
ON CONFLICT (id) DO UPDATE SET
  nickname  = EXCLUDED.nickname,
  phone_hash = COALESCE(NULLIF(EXCLUDED.phone_hash, ''), users.phone_hash),
  role      = EXCLUDED.role,
  updated_at = NOW();

-- name: GetUserByID :one
SELECT id, nickname, COALESCE(phone_hash, ''), role, created_at FROM users WHERE id = $1;

-- name: GetUserByPhone :one
SELECT id, nickname, COALESCE(phone_hash, ''), role, created_at
FROM users WHERE phone_hash = $1 LIMIT 1;

-- name: AppendLogin :exec
INSERT INTO login_history (user_id, channel, ip, user_agent, jwt_kid, created_at)
VALUES ($1, $2, NULLIF($3, '')::inet, NULLIF($4, ''), NULLIF($5, ''), $6);
