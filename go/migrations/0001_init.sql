-- yunmao 初始 schema（PostgreSQL）
-- 与 docs/finalproductplanning/04-设备接入数据模型与API边界.md 第 5 节对齐。
-- 由 pkg/yunmao/db.Migrate（与 goose 风格 +goose Up/Down 标记兼容）执行。
-- +goose Up

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    phone_hash      TEXT NOT NULL UNIQUE,
    wechat_union_id TEXT,
    nickname        TEXT NOT NULL,
    avatar_url      TEXT,
    role            TEXT NOT NULL DEFAULT 'user',
    risk_level      INT  NOT NULL DEFAULT 0,
    region_id       TEXT NOT NULL DEFAULT 'global',
    app_push_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_users_region ON users (region_id);

CREATE TABLE IF NOT EXISTS cats (
    id            TEXT PRIMARY KEY,
    display_name  TEXT NOT NULL,
    gender        TEXT,
    breed         TEXT,
    birth_date    DATE,
    story         TEXT,
    owner_id      TEXT REFERENCES users(id),
    welfare_profile_id TEXT,
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rooms (
    id                     TEXT PRIMARY KEY,
    cat_id                 TEXT REFERENCES cats(id),
    device_id              TEXT,
    display_name           TEXT NOT NULL,
    description            TEXT,
    city                   TEXT,
    region_id              TEXT NOT NULL DEFAULT 'global',
    visibility             TEXT NOT NULL DEFAULT 'public',
    live_status            TEXT NOT NULL DEFAULT 'offline',
    feeding_status         TEXT NOT NULL DEFAULT 'closed',
    feed_cooldown_seconds  INT  NOT NULL DEFAULT 30,
    no_feed_window_start   TEXT,
    no_feed_window_end     TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_rooms_visibility ON rooms (visibility);

CREATE TABLE IF NOT EXISTS devices (
    id                  TEXT PRIMARY KEY,
    room_id             TEXT REFERENCES rooms(id),
    device_type         TEXT NOT NULL,
    hardware_model      TEXT NOT NULL,
    firmware_version    TEXT,
    certificate_id      TEXT,
    online_status       TEXT NOT NULL DEFAULT 'offline',
    last_seen_at        TIMESTAMPTZ,
    remaining_food_grams INT NOT NULL DEFAULT 0,
    health_status       TEXT NOT NULL DEFAULT 'unknown',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS streams (
    id                TEXT PRIMARY KEY,
    room_id           TEXT NOT NULL REFERENCES rooms(id),
    push_url          TEXT NOT NULL,
    playback_profiles JSONB,
    protocols         JSONB,
    status            TEXT NOT NULL DEFAULT 'offline',
    last_keyframe_at  TIMESTAMPTZ,
    qoe_summary       JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS feed_requests (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id),
    room_id         TEXT NOT NULL REFERENCES rooms(id),
    cat_id          TEXT REFERENCES cats(id),
    device_id       TEXT REFERENCES devices(id),
    amount_grams    INT NOT NULL,
    status          TEXT NOT NULL,
    reject_reason   TEXT,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_feed_requests_idem
    ON feed_requests (user_id, idempotency_key);

CREATE TABLE IF NOT EXISTS device_commands (
    id              TEXT PRIMARY KEY,
    feed_request_id TEXT REFERENCES feed_requests(id),
    device_id       TEXT NOT NULL REFERENCES devices(id),
    command_type    TEXT NOT NULL,
    payload         JSONB,
    signature       TEXT,
    status          TEXT NOT NULL,
    sent_at         TIMESTAMPTZ,
    ack_at          TIMESTAMPTZ,
    result_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    channel      TEXT NOT NULL,
    biz_type     TEXT NOT NULL,
    amount_cny   INT NOT NULL,
    status       TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    paid_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS events_audit (
    id           BIGSERIAL PRIMARY KEY,
    event_type   TEXT NOT NULL,
    subject      TEXT,
    payload      JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_events_audit_type ON events_audit (event_type);

-- +goose Down
DROP TABLE IF EXISTS events_audit;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS device_commands;
DROP TABLE IF EXISTS feed_requests;
DROP TABLE IF EXISTS streams;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS cats;
DROP TABLE IF EXISTS users;
