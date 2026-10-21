-- Agent 运行观测（设计见 docs/design/Agent运行观测/Agent运行观测.md）。
-- 只加列、加表，不改已有结构；新增的列都有默认值，旧代码照常读写。
-- 运行记录 Harness 版本：运行创建时写入，之后不变，版本对比按 harness_key 分组。
ALTER TABLE agent_runs ADD COLUMN prompt_version bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_runs ADD COLUMN tools_hash varchar(12) NOT NULL DEFAULT '';
ALTER TABLE agent_runs ADD COLUMN build_sha varchar(40) NOT NULL DEFAULT '';
ALTER TABLE agent_runs ADD COLUMN harness_key varchar(12) NOT NULL DEFAULT '';

CREATE INDEX idx_agent_runs_harness ON agent_runs USING btree (harness_key, created_at);

-- 模型调用补充用途、渠道、耗时和结束原因；耗时只用来分析，不参与计费。
ALTER TABLE agent_model_calls ADD COLUMN purpose varchar(16) NOT NULL DEFAULT 'main';
ALTER TABLE agent_model_calls ADD COLUMN channel_key varchar(64) NOT NULL DEFAULT '';
ALTER TABLE agent_model_calls ADD COLUMN upstream_model varchar(128) NOT NULL DEFAULT '';
ALTER TABLE agent_model_calls ADD COLUMN latency_ms bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_model_calls ADD COLUMN ttft_ms bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_model_calls ADD COLUMN stop_reason varchar(32) NOT NULL DEFAULT '';

-- 工具调用逐条留存：参数和结果是大字段，30 天后由清理任务置空（purged_at 记录清理时间），其余长期保留。
CREATE TABLE agent_tool_calls (
    id bigserial PRIMARY KEY,
    run_id bigint NOT NULL,
    session_id bigint NOT NULL,
    tool_call_id varchar(64) NOT NULL,
    parent_id varchar(64) NOT NULL DEFAULT '',
    name varchar(64) NOT NULL,
    args_json jsonb,
    result_text text,
    result_truncated boolean NOT NULL DEFAULT false,
    is_error boolean NOT NULL DEFAULT false,
    summary varchar(255) NOT NULL DEFAULT '',
    started_at timestamp with time zone NOT NULL,
    ended_at timestamp with time zone,
    duration_ms bigint NOT NULL DEFAULT 0,
    purged_at timestamp with time zone
);

CREATE UNIQUE INDEX uk_agent_tool_calls ON agent_tool_calls USING btree (run_id, tool_call_id);

CREATE INDEX idx_agent_tool_calls_run ON agent_tool_calls USING btree (run_id, started_at);

-- 用户对一次运行的评价：每个运行每人一条。
CREATE TABLE agent_feedback (
    id bigserial PRIMARY KEY,
    run_id bigint NOT NULL,
    user_id bigint NOT NULL,
    rating bigint NOT NULL,
    reasons jsonb NOT NULL DEFAULT '[]',
    comment varchar(500) NOT NULL DEFAULT '',
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE UNIQUE INDEX uk_agent_feedback ON agent_feedback USING btree (run_id, user_id);

-- 运行摘要：一次运行一行，长期保留；后台列表、版本对比和本地 CLI 的索引都只查它。
CREATE TABLE agent_run_digests (
    run_id bigint PRIMARY KEY,
    session_id bigint NOT NULL,
    user_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    mode varchar(16) NOT NULL DEFAULT '',
    model_key varchar(128) NOT NULL DEFAULT '',
    prompt_version bigint NOT NULL DEFAULT 0,
    tools_hash varchar(12) NOT NULL DEFAULT '',
    build_sha varchar(40) NOT NULL DEFAULT '',
    harness_key varchar(12) NOT NULL DEFAULT '',
    skills jsonb NOT NULL DEFAULT '{}',
    status varchar(20) NOT NULL DEFAULT '',
    error_code varchar(64) NOT NULL DEFAULT '',
    request_head varchar(200) NOT NULL DEFAULT '',
    steps bigint NOT NULL DEFAULT 0,
    tool_calls bigint NOT NULL DEFAULT 0,
    tool_errors bigint NOT NULL DEFAULT 0,
    model_calls bigint NOT NULL DEFAULT 0,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    spent_credits bigint NOT NULL DEFAULT 0,
    duration_ms bigint NOT NULL DEFAULT 0,
    approvals_total bigint NOT NULL DEFAULT 0,
    approvals_rejected bigint NOT NULL DEFAULT 0,
    steer_count bigint NOT NULL DEFAULT 0,
    mutation_count bigint NOT NULL DEFAULT 0,
    undone boolean NOT NULL DEFAULT false,
    feedback bigint NOT NULL DEFAULT 0,
    runtime_tail text,
    ended_at timestamp with time zone,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE INDEX idx_agent_run_digests_user_id ON agent_run_digests USING btree (user_id);

CREATE INDEX idx_agent_run_digests_harness ON agent_run_digests USING btree (harness_key, ended_at);

CREATE INDEX idx_agent_run_digests_ended_at ON agent_run_digests USING btree (ended_at);

CREATE INDEX idx_agent_run_digests_updated_at ON agent_run_digests USING btree (updated_at);

-- 分析令牌：后台签发，给本地 agent-trace 只读拉取运行记录。只存明文的 sha256，明文只在签发时显示一次。
CREATE TABLE admin_api_tokens (
    id bigserial PRIMARY KEY,
    name varchar(64) NOT NULL,
    prefix varchar(12) NOT NULL,
    token_hash varchar(64) NOT NULL,
    scope varchar(32) NOT NULL,
    created_by bigint NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    last_used_at timestamp with time zone,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone
);

CREATE UNIQUE INDEX idx_admin_api_tokens_token_hash ON admin_api_tokens USING btree (token_hash);
