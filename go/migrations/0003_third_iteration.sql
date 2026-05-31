-- 第三轮新增：outbox DLQ、device 字段补齐、login 历史、coupons / wallets、jwt kid 跟踪。
-- 与 docs/dev/03-third-iteration-deliverable.md + ADR-0012/0013/0014 对齐。
-- +goose Up

-- outbox 死信表：超过 MaxAttempts 的行被搬到这里，relay 不再处理。
CREATE TABLE IF NOT EXISTS outbox_dlq (
    id              BIGINT PRIMARY KEY,
    topic           TEXT NOT NULL,
    partition_key   TEXT NOT NULL,
    payload         BYTEA NOT NULL,
    headers         JSONB,
    region_id       TEXT NOT NULL DEFAULT 'global',
    reason          TEXT,
    moved_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_outbox_dlq_topic ON outbox_dlq (topic);

-- devices 字段补齐（MQTT 凭证 + last_seen）
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mqtt_credential_hash TEXT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mqtt_username        TEXT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS capability           JSONB DEFAULT '{}'::jsonb;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS region_id            TEXT NOT NULL DEFAULT 'global';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- rooms 字段补齐（stream_key + subscription_policy + cat_ids[] + owner_id）
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS owner_id            TEXT REFERENCES users(id);
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS cat_ids             TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS stream_key          TEXT;
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS subscription_policy JSONB DEFAULT '{}'::jsonb;

-- 登录历史：用户每次登录的 IP / UA / kid。
CREATE TABLE IF NOT EXISTS login_history (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    channel     TEXT NOT NULL DEFAULT 'sms',
    ip          INET,
    user_agent  TEXT,
    jwt_kid     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_login_history_uid ON login_history (user_id, created_at DESC);

-- 平台 JWT 签名密钥 KID 注册表（user-svc / room-svc 写入；gateway 通过 JWKS 同步）
CREATE TABLE IF NOT EXISTS jwt_keys (
    kid         TEXT PRIMARY KEY,
    alg         TEXT NOT NULL,
    public_pem  TEXT NOT NULL,
    not_before  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    not_after   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- coupons / wallets：billing-svc 最小骨架（不接真实支付）
CREATE TABLE IF NOT EXISTS coupons (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    kind         TEXT NOT NULL,
    amount_cny   INT  NOT NULL DEFAULT 0,
    feed_credits INT  NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'active',
    issued_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ,
    consumed_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_coupons_user ON coupons (user_id, status);

CREATE TABLE IF NOT EXISTS wallets (
    user_id        TEXT PRIMARY KEY REFERENCES users(id),
    cny_balance    BIGINT NOT NULL DEFAULT 0,
    feed_credits   INT    NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- orders 字段补齐：region_id + idempotency_key + refunded_at
ALTER TABLE orders ADD COLUMN IF NOT EXISTS region_id        TEXT NOT NULL DEFAULT 'global';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS idempotency_key  TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS refunded_at      TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW();
CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_user_idem
    ON orders (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS wallets;
DROP TABLE IF EXISTS coupons;
DROP TABLE IF EXISTS jwt_keys;
DROP TABLE IF EXISTS login_history;
DROP TABLE IF EXISTS outbox_dlq;
ALTER TABLE devices DROP COLUMN IF EXISTS mqtt_credential_hash;
ALTER TABLE devices DROP COLUMN IF EXISTS mqtt_username;
ALTER TABLE devices DROP COLUMN IF EXISTS capability;
ALTER TABLE devices DROP COLUMN IF EXISTS region_id;
ALTER TABLE devices DROP COLUMN IF EXISTS updated_at;
ALTER TABLE rooms DROP COLUMN IF EXISTS owner_id;
ALTER TABLE rooms DROP COLUMN IF EXISTS cat_ids;
ALTER TABLE rooms DROP COLUMN IF EXISTS stream_key;
ALTER TABLE rooms DROP COLUMN IF EXISTS subscription_policy;
ALTER TABLE orders DROP COLUMN IF EXISTS region_id;
ALTER TABLE orders DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE orders DROP COLUMN IF EXISTS refunded_at;
ALTER TABLE orders DROP COLUMN IF EXISTS updated_at;
