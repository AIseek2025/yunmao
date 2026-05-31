-- 第二轮新增：事务性 outbox、事件溯源、投喂安全配置、账户余额。
-- 与 docs/dev/02-second-iteration-deliverable.md 第 2.B 节 + ADR-0006 对齐。
-- +goose Up

CREATE TABLE IF NOT EXISTS accounts (
    user_id        TEXT PRIMARY KEY REFERENCES users(id),
    region_id      TEXT NOT NULL DEFAULT 'global',
    feed_credits   INT  NOT NULL DEFAULT 0,
    feed_quota_day INT  NOT NULL DEFAULT 6,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- feed_requests 字段补齐：device_command_id、updated_at、region_id；不破坏 0001 的索引。
ALTER TABLE feed_requests ADD COLUMN IF NOT EXISTS device_command_id TEXT;
ALTER TABLE feed_requests ADD COLUMN IF NOT EXISTS region_id   TEXT NOT NULL DEFAULT 'global';
ALTER TABLE feed_requests ADD COLUMN IF NOT EXISTS updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- 事件溯源：每次投喂状态变更追加一行；适合做调试时间线 + 重放。
CREATE TABLE IF NOT EXISTS feeding_request_events (
    id              BIGSERIAL PRIMARY KEY,
    feed_request_id TEXT NOT NULL REFERENCES feed_requests(id),
    from_state      TEXT NOT NULL,
    to_state        TEXT NOT NULL,
    reason          TEXT,
    actor           TEXT NOT NULL DEFAULT 'system',
    region_id       TEXT NOT NULL DEFAULT 'global',
    payload         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_feeding_request_events_rid
    ON feeding_request_events (feed_request_id, id);

-- 事务性 outbox：状态变更和事件发布在同一事务内写入；
-- outbox relay 异步把 published_at=NULL 的事件投递到 Kafka，成功后写 published_at。
CREATE TABLE IF NOT EXISTS outbox (
    id              BIGSERIAL PRIMARY KEY,
    topic           TEXT NOT NULL,
    partition_key   TEXT NOT NULL,
    payload         BYTEA NOT NULL,
    headers         JSONB,
    region_id       TEXT NOT NULL DEFAULT 'global',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished
    ON outbox (id) WHERE published_at IS NULL;

-- 投喂安全策略表（admin-svc 写入；feeding-svc 读取并热更新）。
CREATE TABLE IF NOT EXISTS feeding_safety_policies (
    room_id                TEXT PRIMARY KEY,
    room_cooldown_sec      INT NOT NULL,
    user_room_cooldown_sec INT NOT NULL,
    cat_daily_limit        INT NOT NULL,
    no_feed_window_start   TEXT,
    no_feed_window_end     TEXT,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS feeding_safety_policies;
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS feeding_request_events;
DROP TABLE IF EXISTS accounts;
ALTER TABLE feed_requests DROP COLUMN IF EXISTS device_command_id;
ALTER TABLE feed_requests DROP COLUMN IF EXISTS region_id;
ALTER TABLE feed_requests DROP COLUMN IF EXISTS updated_at;
