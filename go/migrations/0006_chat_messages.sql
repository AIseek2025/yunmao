-- 0006_chat_messages.sql
-- 第六轮：弹幕（chat-svc）持久化。
--
-- 字段：
--   id：消息主键（ULID）
--   user_id：发送方
--   room_id：房间
--   body：纯文本（含 emoji codepoint），≤ 256 字符
--   emojis：JSONB 引用（贴图 ID 列表）
--   moderation_status：pending | published | hidden | deleted | flagged
--   moderation_reason：审核原因
--   client_msg_id：客户端去重 key（去重窗口 5 分钟）
--   created_at / updated_at
--
-- 索引：按房间倒序拉，按 (user_id, client_msg_id) 去重。

BEGIN;

CREATE TABLE IF NOT EXISTS chat_messages (
    id                  TEXT PRIMARY KEY,
    user_id             TEXT NOT NULL,
    room_id             TEXT NOT NULL,
    body                TEXT NOT NULL,
    emojis              JSONB NOT NULL DEFAULT '[]'::jsonb,
    moderation_status   TEXT NOT NULL DEFAULT 'published'
        CHECK (moderation_status IN ('pending','published','hidden','deleted','flagged')),
    moderation_reason   TEXT,
    client_msg_id       TEXT,
    region_id           TEXT NOT NULL DEFAULT 'global',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_room_created_desc
  ON chat_messages(room_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_chat_messages_user_client_msg
  ON chat_messages(user_id, client_msg_id)
  WHERE client_msg_id IS NOT NULL;

COMMIT;
