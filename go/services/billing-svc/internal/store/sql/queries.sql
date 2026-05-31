-- sqlc 查询：billing-svc 落库 SQL（与 internal/store/store.go 中 SQL 一致）。

-- name: CreateOrder :exec
INSERT INTO orders
(id, user_id, channel, biz_type, amount_cny, status, idempotency_key, region_id, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7, ''),$8,$9,$9)
ON CONFLICT (user_id, idempotency_key) DO NOTHING;

-- name: MarkOrderPaid :exec
UPDATE orders
SET status = 'paid', paid_at = $1, updated_at = $1
WHERE id = $2 AND status <> 'paid';

-- name: RefundOrder :exec
UPDATE orders SET status = 'refunded', refunded_at = $1, updated_at = $1
WHERE id = $2 AND status <> 'refunded';

-- name: GetOrder :one
SELECT id, user_id, channel, biz_type, amount_cny, status,
       idempotency_key, region_id, created_at, paid_at, refunded_at, updated_at
FROM orders WHERE id = $1;
