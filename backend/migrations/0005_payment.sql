-- 积分充值与支付：渠道配置、充值商品、支付订单、渠道通知记录，设计见 docs/design/支付功能/支付功能.md。
-- 不用外键：订单只引用用户和商品的 id，商品有订单后只能下架不能删除（由服务保证）；渠道密钥的密文在 ai_secrets（名 payment:<provider>）。

CREATE TABLE payment_channels (
    id bigserial PRIMARY KEY,
    provider varchar(32) NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    config_json jsonb NOT NULL DEFAULT '{}',
    secret_set jsonb NOT NULL DEFAULT '{}',
    order_timeout_minutes bigint NOT NULL DEFAULT 15,
    validated_at timestamp with time zone,
    version bigint NOT NULL DEFAULT 1,
    updated_by bigint NOT NULL DEFAULT 0,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE UNIQUE INDEX uk_payment_channels_provider ON payment_channels USING btree (provider);

CREATE TABLE recharge_products (
    id bigserial PRIMARY KEY,
    name varchar(64) NOT NULL,
    price_fen bigint NOT NULL,
    base_credits bigint NOT NULL,
    bonus_credits bigint NOT NULL DEFAULT 0,
    badge varchar(16) NOT NULL DEFAULT '',
    sort bigint NOT NULL DEFAULT 0,
    on_sale boolean NOT NULL DEFAULT true,
    sold_count bigint NOT NULL DEFAULT 0,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE TABLE payment_orders (
    id bigserial PRIMARY KEY,
    order_no varchar(32) NOT NULL,
    user_id bigint NOT NULL,
    idempotency_key varchar(64) NOT NULL,
    product_id bigint NOT NULL,
    product_snapshot jsonb NOT NULL DEFAULT '{}',
    amount_fen bigint NOT NULL,
    credits bigint NOT NULL,
    method varchar(16) NOT NULL,
    provider varchar(32) NOT NULL,
    channel_version bigint NOT NULL DEFAULT 0,
    status varchar(16) NOT NULL,
    checkout_mode varchar(16) NOT NULL DEFAULT '',
    checkout_value text NOT NULL DEFAULT '',
    expires_at timestamp with time zone NOT NULL,
    provider_trade_no varchar(64) NOT NULL DEFAULT '',
    paid_amount_fen bigint NOT NULL DEFAULT 0,
    paid_at timestamp with time zone,
    credited_at timestamp with time zone,
    closed_at timestamp with time zone,
    close_reason varchar(16) NOT NULL DEFAULT '',
    late_paid boolean NOT NULL DEFAULT false,
    abnormal_reason varchar(32) NOT NULL DEFAULT '',
    fail_reason varchar(255) NOT NULL DEFAULT '',
    last_queried_at timestamp with time zone,
    client_ip varchar(64) NOT NULL DEFAULT '',
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE UNIQUE INDEX uk_payment_orders_no ON payment_orders USING btree (order_no);
-- 同一用户的同一个幂等键只对应一笔订单：重复点击「立即支付」不会多下单
CREATE UNIQUE INDEX uk_payment_orders_idem ON payment_orders USING btree (user_id, idempotency_key);
-- 渠道交易号非空时 (渠道, 交易号) 唯一：同一笔渠道交易不能给两个订单入账
CREATE UNIQUE INDEX uk_payment_orders_trade ON payment_orders USING btree (provider, provider_trade_no) WHERE (provider_trade_no <> '');
CREATE INDEX idx_payment_orders_user ON payment_orders USING btree (user_id, created_at DESC);
-- 补单任务按 (状态, 到期时间) 扫描
CREATE INDEX idx_payment_orders_status_exp ON payment_orders USING btree (status, expires_at);

CREATE TABLE payment_notifications (
    id bigserial PRIMARY KEY,
    provider varchar(32) NOT NULL,
    event_key varchar(128) NOT NULL,
    order_no varchar(32) NOT NULL,
    result varchar(24) NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}',
    received_at timestamp with time zone NOT NULL
);

CREATE UNIQUE INDEX uk_payment_notifications_event ON payment_notifications USING btree (provider, event_key);
CREATE INDEX idx_payment_notifications_order ON payment_notifications USING btree (order_no);

-- 积分流水新增「充值」类型：payment_order_id 非空时 (payment_order_id, type) 唯一，
-- 和 uk_ledger_task_type 同样的思路，是防重复入账的最后一道防线。老流水这一列都是 NULL，不受影响。
ALTER TABLE credit_ledger ADD COLUMN payment_order_id bigint;
CREATE UNIQUE INDEX uk_ledger_payment_type ON credit_ledger USING btree (payment_order_id, type) WHERE (payment_order_id IS NOT NULL);
