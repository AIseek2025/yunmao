-- Snapshot of rooms-related schema as of migration 0004.
-- 真实表 DDL 由 go/migrations/0001 + 0003 + 0004 维护；本文件供 sqlc 类型推导。
CREATE TABLE rooms (
    id                     TEXT PRIMARY KEY,
    cat_id                 TEXT,
    device_id              TEXT,
    owner_id               TEXT,
    display_name           TEXT NOT NULL,
    description            TEXT,
    city                   TEXT,
    region_id              TEXT NOT NULL DEFAULT 'global',
    visibility             TEXT NOT NULL DEFAULT 'public',
    live_status            TEXT NOT NULL DEFAULT 'offline',
    feeding_status         TEXT NOT NULL DEFAULT 'closed',
    status                 TEXT NOT NULL DEFAULT 'offline',
    feed_cooldown_seconds  INT  NOT NULL DEFAULT 30,
    no_feed_window_start   TEXT,
    no_feed_window_end     TEXT,
    stream_key             TEXT,
    stream_key_rotated_at  TIMESTAMPTZ,
    cat_ids                TEXT[] NOT NULL DEFAULT '{}',
    subscription_policy    JSONB,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
