-- Snapshot of user-svc related schema as of migration 0004.
CREATE TABLE users (
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

CREATE TABLE login_history (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    channel     TEXT NOT NULL DEFAULT 'sms',
    ip          INET,
    user_agent  TEXT,
    jwt_kid     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
