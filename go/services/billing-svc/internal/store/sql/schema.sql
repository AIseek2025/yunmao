-- Snapshot of billing-svc related schema as of migration 0004.
CREATE TABLE orders (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    channel         TEXT NOT NULL,
    biz_type        TEXT NOT NULL,
    amount_cny      INT NOT NULL,
    status          TEXT NOT NULL,
    idempotency_key TEXT,
    region_id       TEXT NOT NULL DEFAULT 'global',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    paid_at         TIMESTAMPTZ,
    refunded_at     TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE coupons (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    kind         TEXT NOT NULL,
    amount_cny   INT  NOT NULL DEFAULT 0,
    feed_credits INT  NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'active',
    issued_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ,
    consumed_at  TIMESTAMPTZ
);

CREATE TABLE wallets (
    user_id        TEXT PRIMARY KEY,
    cny_balance    BIGINT NOT NULL DEFAULT 0,
    feed_credits   INT    NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
