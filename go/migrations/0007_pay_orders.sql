-- 第七轮（E）：支付渠道订单落库。
-- pay_orders 记录 billing-svc 与支付渠道交互的快照：
--   - 创建 prepay 时插入一条 pending 行；
--   - 收到 webhook（paid/refunded/closed/failed）时 UPDATE；
--   - external_trade_no + channel 唯一约束 → 幂等防重放。
CREATE TABLE IF NOT EXISTS pay_orders (
    id                  TEXT PRIMARY KEY,
    order_id            TEXT NOT NULL,
    channel             TEXT NOT NULL,
    prepay_id           TEXT,
    external_trade_no   TEXT,
    amount_fen          BIGINT NOT NULL,
    currency            TEXT NOT NULL DEFAULT 'CNY',
    status              TEXT NOT NULL DEFAULT 'pending', -- pending|paid|refunded|closed|failed
    raw_payload         JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at             TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS pay_orders_channel_external_uniq
    ON pay_orders (channel, external_trade_no)
    WHERE external_trade_no IS NOT NULL;
CREATE INDEX IF NOT EXISTS pay_orders_order_id_idx ON pay_orders (order_id);
CREATE INDEX IF NOT EXISTS pay_orders_status_idx ON pay_orders (status);
