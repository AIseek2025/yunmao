-- sqlc 查询：room-svc 落库 SQL（与 internal/store/store.go 中 const 一致）。
-- 跑 sqlc generate 时输出到 internal/store/gen/。手写 pgx 实现保留以避免 CI 强依赖 sqlc 二进制。

-- name: GetRoom :one
SELECT id, COALESCE(cat_id, ''), COALESCE(device_id, ''), COALESCE(owner_id, ''),
       display_name, COALESCE(description, ''), COALESCE(city, ''), region_id,
       visibility, live_status, feeding_status, COALESCE(status, 'offline'),
       feed_cooldown_seconds, COALESCE(no_feed_window_start, ''), COALESCE(no_feed_window_end, ''),
       COALESCE(stream_key, ''), stream_key_rotated_at,
       COALESCE(cat_ids, '{}'::text[]),
       created_at, updated_at
FROM rooms WHERE id = $1;

-- name: CreateRoom :exec
INSERT INTO rooms
(id, cat_id, device_id, owner_id, display_name, description, city, region_id,
 visibility, live_status, feeding_status, status, feed_cooldown_seconds,
 no_feed_window_start, no_feed_window_end, stream_key, cat_ids, created_at, updated_at)
VALUES
($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), $5, NULLIF($6,''), NULLIF($7,''), $8,
 $9, $10, $11, $12, $13,
 NULLIF($14,''), NULLIF($15,''), NULLIF($16,''), $17, $18, $18)
ON CONFLICT (id) DO NOTHING;

-- name: UpdateRoom :exec
UPDATE rooms SET
  cat_id = NULLIF($2,''),
  device_id = NULLIF($3,''),
  owner_id = NULLIF($4,''),
  display_name = $5,
  description = NULLIF($6,''),
  city = NULLIF($7,''),
  region_id = $8,
  visibility = $9,
  live_status = $10,
  feeding_status = $11,
  status = $12,
  feed_cooldown_seconds = $13,
  no_feed_window_start = NULLIF($14,''),
  no_feed_window_end = NULLIF($15,''),
  cat_ids = $16,
  updated_at = NOW()
WHERE id = $1;

-- name: SetRoomStatus :exec
UPDATE rooms SET status = $2, live_status = $3, updated_at = NOW() WHERE id = $1;

-- name: SetStreamKey :exec
UPDATE rooms SET stream_key = $2, stream_key_rotated_at = $3, updated_at = NOW() WHERE id = $1;

-- name: ListRooms :many
SELECT id, COALESCE(cat_id, ''), COALESCE(device_id, ''), COALESCE(owner_id, ''),
       display_name, COALESCE(description, ''), COALESCE(city, ''), region_id,
       visibility, live_status, feeding_status, COALESCE(status, 'offline'),
       feed_cooldown_seconds, COALESCE(no_feed_window_start, ''), COALESCE(no_feed_window_end, ''),
       COALESCE(stream_key, ''), stream_key_rotated_at,
       COALESCE(cat_ids, '{}'::text[]),
       created_at, updated_at
FROM rooms
WHERE ($1::text = '' OR owner_id = $1)
  AND ($2::text = '' OR region_id = $2)
  AND ($3::text = '' OR COALESCE(status, 'offline') = $3)
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;
