-- sqlc 查询：feeding-svc 落库 SQL。
-- 与 internal/store/store.go 中 const 一致；手写 pgx 实现保留。

-- name: CreateFeedRequest :exec
INSERT INTO feed_requests
(id, user_id, room_id, cat_id, device_id, device_command_id, amount_grams,
 status, idempotency_key, region_id, created_at, updated_at)
VALUES ($1,$2,$3,NULLIF($4, ''),NULLIF($5, ''),$6,$7,$8,$9,$10,$11,$11)
ON CONFLICT (user_id, idempotency_key) DO NOTHING;

-- name: UpdateFeedRequest :exec
UPDATE feed_requests
SET status      = $1,
    reject_reason = COALESCE(NULLIF($2, ''), reject_reason),
    updated_at  = NOW()
WHERE id = $3;

-- name: CancelFeedRequest :exec
UPDATE feed_requests
SET status = 'rejected',
    reject_reason = COALESCE(NULLIF($2, ''), 'cancelled'),
    cancelled_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND status IN ('queued', 'dispatched');

-- name: TimeoutFeedRequest :exec
UPDATE feed_requests
SET status = 'failed',
    reject_reason = 'timeout',
    timeout_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND status IN ('dispatched', 'queued');

-- name: GetFeedRequest :one
SELECT id, user_id, room_id, COALESCE(cat_id, ''), COALESCE(device_id, ''),
       COALESCE(device_command_id, ''), amount_grams, status, idempotency_key,
       COALESCE(reject_reason, ''), region_id, created_at, updated_at
FROM feed_requests WHERE id = $1;

-- name: ListByRoom :many
SELECT id, user_id, room_id, COALESCE(cat_id, ''), COALESCE(device_id, ''),
       COALESCE(device_command_id, ''), amount_grams, status, idempotency_key,
       COALESCE(reject_reason, ''), region_id, created_at, updated_at
FROM feed_requests WHERE room_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ListExpiredDispatched :many
SELECT id, room_id, COALESCE(device_id, ''), COALESCE(device_command_id, ''),
       status, updated_at
FROM feed_requests
WHERE status IN ('dispatched','queued') AND updated_at < $1
LIMIT $2;

-- name: GetFlag :one
SELECT name, enabled, scope, COALESCE(value, '{}'::jsonb), updated_at
FROM feature_flags WHERE name = $1;

-- name: SetFlag :exec
INSERT INTO feature_flags (name, enabled, scope, value, note, updated_at, updated_by)
VALUES ($1, $2, $3, $4::jsonb, $5, NOW(), $6)
ON CONFLICT (name) DO UPDATE SET
  enabled = EXCLUDED.enabled,
  scope = EXCLUDED.scope,
  value = EXCLUDED.value,
  note = COALESCE(EXCLUDED.note, feature_flags.note),
  updated_at = NOW(),
  updated_by = EXCLUDED.updated_by;

-- name: AppendEvent :exec
INSERT INTO feeding_request_events
(feed_request_id, from_state, to_state, reason, actor, region_id, payload)
VALUES ($1,$2,$3,$4,$5,$6,$7);

-- name: InsertOutbox :one
INSERT INTO outbox (topic, partition_key, payload, headers, region_id)
VALUES ($1,$2,$3,$4::jsonb,$5)
RETURNING id;
