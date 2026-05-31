-- 第八轮（C）：弹幕审核本地词表（热更新）。
-- 决策见 ADR-0026。

CREATE TABLE IF NOT EXISTS chat_wordlists (
    id          BIGSERIAL PRIMARY KEY,
    region      TEXT NOT NULL DEFAULT 'global',
    language    TEXT NOT NULL DEFAULT 'zh',
    word        TEXT NOT NULL,
    action      TEXT NOT NULL DEFAULT 'hide',          -- pass/warn/hide/block/recall
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at  TIMESTAMPTZ DEFAULT now() NOT NULL,
    UNIQUE (region, language, word)
);

CREATE INDEX IF NOT EXISTS idx_chat_wordlists_region_lang
    ON chat_wordlists (region, language);

CREATE INDEX IF NOT EXISTS idx_chat_wordlists_updated_at
    ON chat_wordlists (updated_at DESC);

-- 词表更新事件（chat.wordlist.updated）→ chat-svc / gateway 订阅刷缓存。
-- 通过 outbox 落 PG → relay 推 Kafka。无需新表，复用 outbox_events。
