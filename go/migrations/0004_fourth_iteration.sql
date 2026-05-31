-- 第四轮新增：room/device store 补齐、feeding 取消/超时、feature_flags、kms_key_versions。
-- 与 docs/dev/04-fourth-iteration-deliverable.md + ADR-0015 / 0016 对齐。
-- +goose Up

-- rooms 表补齐：status 与 streaming 字段已经在 0001/0003 引入，本轮补齐
-- 索引 + owner_id NOT NULL 默认 + 关于 ban 状态使用的字段。
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'offline';
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS stream_key_rotated_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_rooms_owner ON rooms (owner_id);
CREATE INDEX IF NOT EXISTS idx_rooms_region_status ON rooms (region_id, status);

-- devices 表补齐：mqtt_credential_expires_at 用于短期凭证轮换；index 加速 ListByOwner。
ALTER TABLE devices ADD COLUMN IF NOT EXISTS owner_id TEXT REFERENCES users(id);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mqtt_credential_expires_at TIMESTAMPTZ;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS firmware_target TEXT;
CREATE INDEX IF NOT EXISTS idx_devices_owner ON devices (owner_id);
CREATE INDEX IF NOT EXISTS idx_devices_room ON devices (room_id);

-- feeding 取消 / 超时补偿：复用 status 字段，但用 reject_reason 字段记录 reason。
-- 这里只新增辅助字段；状态机由 service/feedstate 控制。
ALTER TABLE feed_requests ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ;
ALTER TABLE feed_requests ADD COLUMN IF NOT EXISTS timeout_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_feed_requests_status_updated
  ON feed_requests (status, updated_at);

-- feature_flags：灰度开关。运营 / admin 可改。
CREATE TABLE IF NOT EXISTS feature_flags (
    name         TEXT PRIMARY KEY,
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    scope        TEXT NOT NULL DEFAULT 'global', -- global | region:xx | room:xx | device:xx
    value        JSONB DEFAULT '{}'::jsonb,
    note         TEXT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   TEXT
);

-- 默认开关：让 dev 直接可用。
INSERT INTO feature_flags (name, enabled, scope, value, note)
VALUES
  ('feeding.allow_new_rooms',     TRUE,  'global', '{}'::jsonb,
   '是否允许新建房间提交投喂（false 时直接拒绝）'),
  ('feeding.region_qps_limit',    TRUE,  'global', '{"per_region": 200}'::jsonb,
   '单 region 每秒投喂上限'),
  ('feeding.device_maintenance',  FALSE, 'global', '{"device_ids": []}'::jsonb,
   '维护模式中的设备列表（拒绝投喂）'),
  ('feeding.timeout_seconds',     TRUE,  'global', '{"seconds": 30}'::jsonb,
   'dispatched/executing 超过 N 秒标记 timeout 并发补偿事件')
ON CONFLICT (name) DO NOTHING;

-- kms_key_versions：KeyProvider 轮换记录（MockKmsProvider 持久化用；Vault/AWS 用 KMS 自己 keyring）。
CREATE TABLE IF NOT EXISTS kms_key_versions (
    kid           TEXT PRIMARY KEY,
    alg           TEXT NOT NULL,           -- HS256 | RS256 | ES256
    state         TEXT NOT NULL DEFAULT 'active', -- active | retiring | retired
    public_pem    TEXT,
    private_pem   TEXT, -- 仅 MockKms 持久化；生产由 KMS 内部持有
    not_before    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    not_after     TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_from  TEXT
);
CREATE INDEX IF NOT EXISTS idx_kms_state ON kms_key_versions (state);

-- +goose Down
DROP TABLE IF EXISTS kms_key_versions;
DROP TABLE IF EXISTS feature_flags;
ALTER TABLE feed_requests DROP COLUMN IF EXISTS cancelled_at;
ALTER TABLE feed_requests DROP COLUMN IF EXISTS timeout_at;
ALTER TABLE devices DROP COLUMN IF EXISTS owner_id;
ALTER TABLE devices DROP COLUMN IF EXISTS mqtt_credential_expires_at;
ALTER TABLE devices DROP COLUMN IF EXISTS firmware_target;
ALTER TABLE rooms DROP COLUMN IF EXISTS status;
ALTER TABLE rooms DROP COLUMN IF EXISTS stream_key_rotated_at;
