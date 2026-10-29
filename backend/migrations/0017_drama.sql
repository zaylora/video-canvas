-- 短剧工作室一期：原文、分集剧本与持久流水线；资产仍保存在专属画布。
-- 与已有素材引用一致，不建跨资源外键；归属和级联删除在业务事务中校验。
CREATE TABLE drama_projects (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    deleted_at timestamptz,
    user_id bigint NOT NULL,
    canvas_project_id bigint NOT NULL,
    source_asset_id bigint,
    text_model_id bigint,
    title varchar(255) NOT NULL,
    source_text text NOT NULL,
    source_kind varchar(16) NOT NULL DEFAULT '',
    source_summary text NOT NULL DEFAULT '',
    genre varchar(32) NOT NULL DEFAULT '',
    style_key varchar(32) NOT NULL,
    style_custom text NOT NULL DEFAULT '',
    ratio varchar(8) NOT NULL DEFAULT '',
    target_episodes bigint NOT NULL DEFAULT 0,
    episode_seconds bigint NOT NULL DEFAULT 60,
    text_model varchar(128) NOT NULL DEFAULT '',
    outline_md text NOT NULL DEFAULT '',
    characters_md text NOT NULL DEFAULT '',
    status varchar(16) NOT NULL DEFAULT 'analyzing',
    revision bigint NOT NULL DEFAULT 1
);
CREATE INDEX idx_drama_projects_deleted_at ON drama_projects (deleted_at);
CREATE INDEX idx_drama_projects_user_id ON drama_projects (user_id);
CREATE INDEX idx_drama_user_updated ON drama_projects (user_id, updated_at DESC);
CREATE UNIQUE INDEX uk_drama_canvas ON drama_projects (canvas_project_id);

CREATE TABLE drama_episodes (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    deleted_at timestamptz,
    user_id bigint NOT NULL,
    drama_id bigint NOT NULL,
    no bigint NOT NULL,
    title varchar(255) NOT NULL DEFAULT '',
    synopsis text NOT NULL DEFAULT '',
    script_md text NOT NULL DEFAULT '',
    script_status varchar(16) NOT NULL DEFAULT 'empty',
    storyboard_status varchar(16) NOT NULL DEFAULT 'empty',
    final_asset_id bigint,
    locked_at timestamptz,
    revision bigint NOT NULL DEFAULT 1
);
CREATE INDEX idx_drama_episodes_deleted_at ON drama_episodes (deleted_at);
CREATE INDEX idx_drama_episodes_user_id ON drama_episodes (user_id);
CREATE UNIQUE INDEX uk_drama_episode_no ON drama_episodes (drama_id, no);

CREATE TABLE drama_jobs (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    deleted_at timestamptz,
    user_id bigint NOT NULL,
    drama_id bigint NOT NULL,
    step varchar(16) NOT NULL,
    episodes jsonb NOT NULL DEFAULT '[]'::jsonb,
    status varchar(16) NOT NULL DEFAULT 'queued',
    progress bigint NOT NULL DEFAULT 0,
    credits bigint NOT NULL DEFAULT 0,
    frozen_credits bigint NOT NULL DEFAULT 0,
    error_code bigint NOT NULL DEFAULT 0,
    error_message text NOT NULL DEFAULT '',
    config_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    input_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    model_id bigint NOT NULL DEFAULT 0,
    output jsonb NOT NULL DEFAULT '{}'::jsonb,
    started_at timestamptz,
    finished_at timestamptz
);
CREATE INDEX idx_drama_jobs_deleted_at ON drama_jobs (deleted_at);
CREATE INDEX idx_drama_jobs_user_id ON drama_jobs (user_id);
CREATE INDEX idx_drama_jobs_drama_id ON drama_jobs (drama_id);
-- 数据库约束是跨实例并发闸门，应用层项目行锁给出稳定业务错误。
CREATE UNIQUE INDEX uk_drama_active_job ON drama_jobs (drama_id) WHERE status = 'queued' OR status = 'running';

ALTER TABLE canvas_projects ADD COLUMN drama_id bigint;
CREATE INDEX idx_canvas_projects_drama_id ON canvas_projects (drama_id);
ALTER TABLE character_cards ADD COLUMN drama_id bigint;
ALTER TABLE character_cards ADD COLUMN look_name varchar(64) NOT NULL DEFAULT '';
ALTER TABLE character_cards ADD COLUMN voice_prompt text NOT NULL DEFAULT '';
ALTER TABLE character_cards ALTER COLUMN avatar_asset_id DROP NOT NULL;
DROP INDEX uk_character_cards_user_name;
CREATE INDEX idx_character_cards_drama_id ON character_cards (drama_id);

-- 短剧与生成任务共享积分账户，但流水具有独立任务引用，避免与生成任务 ID 碰撞。
ALTER TABLE credit_ledger ADD COLUMN drama_job_id bigint;
CREATE UNIQUE INDEX uk_ledger_drama_type ON credit_ledger (drama_job_id, type) WHERE drama_job_id IS NOT NULL;
