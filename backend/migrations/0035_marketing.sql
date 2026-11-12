-- 营销工作室：项目、轮次、环节运行、脚本；写脚本按环节冻结 / 结算积分，流水关联环节运行。
-- 与已有表一致不建跨资源外键；素材、模型引用靠业务校验。
CREATE TABLE marketing_projects (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    deleted_at timestamptz,
    user_id bigint NOT NULL,
    title varchar(255) NOT NULL DEFAULT '',
    product jsonb,
    product_asset_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    model_asset_id bigint,
    cover_asset_id bigint,
    revision bigint NOT NULL DEFAULT 1
);
CREATE INDEX idx_marketing_projects_deleted_at ON marketing_projects (deleted_at);
CREATE INDEX idx_mkt_project_user_updated ON marketing_projects (user_id, updated_at DESC);

CREATE TABLE marketing_rounds (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    project_id bigint NOT NULL,
    no bigint NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'scripting',
    error text NOT NULL DEFAULT '',
    warnings jsonb NOT NULL DEFAULT '[]'::jsonb,
    settings jsonb NOT NULL,
    ref_video_asset_id bigint,
    model_asset_id bigint,
    plan_count bigint NOT NULL DEFAULT 0,
    frozen bigint NOT NULL DEFAULT 0,
    charged bigint NOT NULL DEFAULT 0,
    reverse jsonb,
    reverse_original jsonb,
    reverse_revision bigint NOT NULL DEFAULT 0,
    follow_basis jsonb,
    created_at timestamptz,
    updated_at timestamptz
);
CREATE INDEX idx_marketing_rounds_user_id ON marketing_rounds (user_id);
CREATE UNIQUE INDEX uk_mkt_round_project_no ON marketing_rounds (project_id, no);

CREATE TABLE marketing_step_runs (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    round_id bigint NOT NULL,
    step_key text NOT NULL,
    method jsonb NOT NULL DEFAULT '{}'::jsonb,
    text_model_id text NOT NULL DEFAULT '',
    status varchar(16) NOT NULL DEFAULT 'queued',
    error text NOT NULL DEFAULT '',
    attempts bigint NOT NULL DEFAULT 0,
    frozen bigint NOT NULL DEFAULT 0,
    charged bigint NOT NULL DEFAULT 0,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz
);
CREATE INDEX idx_mkt_run_round_step ON marketing_step_runs (round_id, step_key, created_at);
CREATE INDEX idx_mkt_run_step_created ON marketing_step_runs (step_key, created_at);

CREATE TABLE marketing_scripts (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    project_id bigint NOT NULL,
    round_id bigint NOT NULL,
    step_run_id bigint NOT NULL,
    kind varchar(16) NOT NULL,
    angle varchar(16) NOT NULL,
    seq bigint NOT NULL DEFAULT 0,
    title text NOT NULL DEFAULT '',
    hook text NOT NULL DEFAULT '',
    shots jsonb NOT NULL,
    music text NOT NULL DEFAULT '',
    voice text NOT NULL DEFAULT '',
    flags jsonb NOT NULL DEFAULT '[]'::jsonb,
    follow_map jsonb,
    original jsonb NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'draft',
    task_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    error text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz,
    updated_at timestamptz
);
CREATE INDEX idx_marketing_scripts_user_id ON marketing_scripts (user_id);
CREATE INDEX idx_marketing_scripts_project_id ON marketing_scripts (project_id);
CREATE INDEX idx_marketing_scripts_round_id ON marketing_scripts (round_id);
CREATE INDEX idx_mkt_script_project_created ON marketing_scripts (project_id, created_at DESC);

-- 写脚本的每个环节运行单独冻结、结算；(marketing_step_run_id, type) 唯一保证冻结、结算、退款各最多一条。
ALTER TABLE credit_ledger ADD COLUMN marketing_step_run_id bigint;
CREATE UNIQUE INDEX uk_ledger_marketing_run_type ON credit_ledger (marketing_step_run_id, type) WHERE marketing_step_run_id IS NOT NULL;
