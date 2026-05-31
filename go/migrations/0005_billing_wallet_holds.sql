-- 0005_billing_wallet_holds.sql
-- 第六轮：billing-svc 加入 Reserve/Confirm/Cancel saga，引入 wallet_holds + wallet_balances。
--
-- wallet_balances：每个 user_id 持有的余额（fen 分）；新增用户的行由 service 层第一次 Reserve 时 INSERT。
-- wallet_holds：feed 请求触发的 Reserve 行；状态机：reserved → confirmed | cancelled | expired。
-- 与 ADR-0010 outbox 一致：所有状态机迁移 + outbox 行同事务。

BEGIN;

CREATE TABLE IF NOT EXISTS wallet_balances (
    user_id          TEXT PRIMARY KEY,
    balance_fen      BIGINT NOT NULL DEFAULT 0 CHECK (balance_fen >= 0),
    reserved_fen     BIGINT NOT NULL DEFAULT 0 CHECK (reserved_fen >= 0),
    region_id        TEXT NOT NULL DEFAULT 'global',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wallet_balances_region ON wallet_balances(region_id);

CREATE TABLE IF NOT EXISTS wallet_holds (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    room_id          TEXT NOT NULL,
    cat_id           TEXT NOT NULL,
    amount_fen       BIGINT NOT NULL CHECK (amount_fen > 0),
    amount_grams     INTEGER NOT NULL,
    idempotency_key  TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'reserved'
        CHECK (status IN ('reserved', 'confirmed', 'cancelled', 'expired')),
    feed_request_id  TEXT,
    region_id        TEXT NOT NULL DEFAULT 'global',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ NOT NULL,
    UNIQUE (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_wallet_holds_user_status ON wallet_holds(user_id, status);
CREATE INDEX IF NOT EXISTS idx_wallet_holds_expires_at ON wallet_holds(expires_at)
  WHERE status = 'reserved';

COMMIT;
