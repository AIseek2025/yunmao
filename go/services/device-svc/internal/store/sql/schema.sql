-- Snapshot of devices-related schema as of migration 0004.
-- 真实表 DDL 由 go/migrations/0001 + 0003 + 0004 维护；本文件供 sqlc 类型推导。
CREATE TABLE devices (
    id                          TEXT PRIMARY KEY,
    room_id                     TEXT,
    owner_id                    TEXT,
    device_type                 TEXT NOT NULL,
    hardware_model              TEXT NOT NULL,
    firmware_version            TEXT,
    firmware_target             TEXT,
    certificate_id              TEXT,
    online_status               TEXT NOT NULL DEFAULT 'offline',
    last_seen_at                TIMESTAMPTZ,
    remaining_food_grams        INT NOT NULL DEFAULT 0,
    health_status               TEXT NOT NULL DEFAULT 'unknown',
    mqtt_username               TEXT,
    mqtt_credential_hash        TEXT,
    mqtt_credential_expires_at  TIMESTAMPTZ,
    capability                  JSONB DEFAULT '{}'::jsonb,
    region_id                   TEXT NOT NULL DEFAULT 'global',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
