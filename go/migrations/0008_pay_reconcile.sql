-- 第八轮（B）：支付对账记录 + 退款订单。
-- 决策见 ADR-0025。

-- 对账记录表：每小时对账 worker 跑完一次后落盘；diff 记录字段差异。
CREATE TABLE IF NOT EXISTS pay_reconcile_records (
    id              BIGSERIAL PRIMARY KEY,
    run_id          TEXT NOT NULL,                     -- 对账批次 ID
    channel         TEXT NOT NULL,                     -- wechat/alipay/appleiap/mock
    order_id        TEXT NOT NULL,
    external_trade_no TEXT,
    local_status    TEXT NOT NULL,                     -- pending/paid/refunded/closed/failed
    remote_status   TEXT,                              -- 渠道侧状态
    local_amount_fen  BIGINT,
    remote_amount_fen BIGINT,
    diff_reason     TEXT,                              -- 空 = 一致；否则 status_mismatch / amount_mismatch / missing
    created_at      TIMESTAMPTZ DEFAULT now() NOT NULL,
    UNIQUE (run_id, channel, order_id)
);

CREATE INDEX IF NOT EXISTS idx_reconcile_diff
    ON pay_reconcile_records (channel, diff_reason)
    WHERE diff_reason IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_reconcile_created ON pay_reconcile_records (created_at DESC);

-- 退款订单：channel refund 异步事件流水。
CREATE TABLE IF NOT EXISTS pay_refund_orders (
    id              BIGSERIAL PRIMARY KEY,
    refund_id       TEXT NOT NULL UNIQUE,              -- 渠道返回 ID
    order_id        TEXT NOT NULL,                     -- 关联 orders.id
    channel         TEXT NOT NULL,
    amount_fen      BIGINT NOT NULL,
    status          TEXT NOT NULL,                     -- ok/pending/failed
    reason          TEXT,
    requested_at    TIMESTAMPTZ DEFAULT now() NOT NULL,
    settled_at      TIMESTAMPTZ,
    raw_response    JSONB
);

CREATE INDEX IF NOT EXISTS idx_refund_order ON pay_refund_orders (order_id);
CREATE INDEX IF NOT EXISTS idx_refund_channel_status ON pay_refund_orders (channel, status);
