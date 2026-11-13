-- 环节协议层（stepkit）：环节的方法绑定、绑定历史和后台试跑。
-- 与已有表一致不建跨资源外键：技能被整个删除时绑定留着，运行时按「技能已删除」退回内置方法。
CREATE TABLE step_bindings (
    step_key text PRIMARY KEY,
    source text NOT NULL,
    skill_id bigint,
    skill_version bigint,
    trial_id bigint,
    updated_by bigint NOT NULL DEFAULT 0,
    updated_at timestamptz
);

CREATE TABLE step_binding_history (
    id bigserial PRIMARY KEY,
    step_key text NOT NULL,
    from_binding jsonb NOT NULL,
    to_binding jsonb NOT NULL,
    trial_id bigint,
    actor_id bigint NOT NULL DEFAULT 0,
    created_at timestamptz
);
CREATE INDEX idx_step_binding_history_step ON step_binding_history (step_key, created_at DESC);

CREATE TABLE step_trials (
    id bigserial PRIMARY KEY,
    step_key text NOT NULL,
    source text NOT NULL,
    skill_id bigint,
    skill_version bigint,
    sample_key text NOT NULL,
    text_model_id text NOT NULL,
    compare_of bigint,
    status text NOT NULL DEFAULT 'queued',
    passed boolean NOT NULL DEFAULT false,
    errors jsonb NOT NULL DEFAULT '[]'::jsonb,
    flags jsonb NOT NULL DEFAULT '[]'::jsonb,
    output jsonb,
    method jsonb,
    error text NOT NULL DEFAULT '',
    attempts bigint NOT NULL DEFAULT 0,
    duration_ms bigint NOT NULL DEFAULT 0,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    credits bigint NOT NULL DEFAULT 0,
    frozen bigint NOT NULL DEFAULT 0,
    created_by bigint NOT NULL,
    created_at timestamptz,
    finished_at timestamptz
);
CREATE INDEX idx_step_trials_step_created ON step_trials (step_key, created_at DESC);

-- 试跑费用从操作的管理员账户扣，流水关联试跑；(step_trial_id, type) 唯一保证冻结、结算、退款各最多一条。
ALTER TABLE credit_ledger ADD COLUMN step_trial_id bigint;
CREATE UNIQUE INDEX uk_ledger_step_trial_type ON credit_ledger (step_trial_id, type) WHERE step_trial_id IS NOT NULL;
