-- Snapshot of feeding-svc related schema as of migration 0004.
-- 真实表 DDL 由 go/migrations/0001 + 0002 + 0004 维护；本文件供 sqlc 类型推导。
CREATE TABLE feed_requests (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    room_id         TEXT NOT NULL,
    cat_id          TEXT,
    device_id       TEXT,
    device_command_id TEXT,
    amount_grams    INT NOT NULL,
    status          TEXT NOT NULL,
    reject_reason   TEXT,
    idempotency_key TEXT NOT NULL,
    region_id       TEXT NOT NULL DEFAULT 'global',
    cancelled_at    TIMESTAMPTZ,
    timeout_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE TABLE feeding_request_events (
    id              BIGSERIAL PRIMARY KEY,
    feed_request_id TEXT NOT NULL,
    from_state      TEXT NOT NULL,
    to_state        TEXT NOT NULL,
    reason          TEXT,
    actor           TEXT NOT NULL DEFAULT 'system',
    region_id       TEXT NOT NULL DEFAULT 'global',
    payload         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE outbox (
    id              BIGSERIAL PRIMARY KEY,
    topic           TEXT NOT NULL,
    partition_key   TEXT NOT NULL,
    payload         BYTEA NOT NULL,
    headers         JSONB,
    region_id       TEXT NOT NULL DEFAULT 'global',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ
);

CREATE TABLE feature_flags (
    name         TEXT PRIMARY KEY,
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    scope        TEXT NOT NULL DEFAULT 'global',
    value        JSONB DEFAULT '{}'::jsonb,
    note         TEXT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   TEXT
);
