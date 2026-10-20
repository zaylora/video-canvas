-- 画布 Agent 运行轨迹（设计见 docs/design/Agent调优Harness/Agent调优Harness.md 第 6.B.2、8.1 节）。
-- 索引放这里，模型请求 / 响应与工具调用的原文压缩后放素材所用的对象存储（key 前缀 agent-traces/），画布快照也在原文对象里，不进表。
-- 老数据：trace_enabled 为 false，后台列表显示为「轨迹关闭」；prompt_version 等快照为空值，不影响任何读路径。

-- 一轮运行的版本快照与轨迹状态
ALTER TABLE agent_runs ADD COLUMN prompt_version bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_runs ADD COLUMN agent_model_key varchar(128) NOT NULL DEFAULT '';
ALTER TABLE agent_runs ADD COLUMN tools_hash varchar(16) NOT NULL DEFAULT '';
ALTER TABLE agent_runs ADD COLUMN skill_pins jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE agent_runs ADD COLUMN input_json jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE agent_runs ADD COLUMN harness_case varchar(128) NOT NULL DEFAULT '';
ALTER TABLE agent_runs ADD COLUMN trace_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE agent_runs ADD COLUMN trace_complete boolean NOT NULL DEFAULT true;
ALTER TABLE agent_runs ADD COLUMN trace_bytes bigint NOT NULL DEFAULT 0;

-- 后台「Agent 运行」列表按创建时间倒序、按时间范围筛选
CREATE INDEX idx_agent_runs_created_at ON agent_runs USING btree (created_at);

-- chat span 的索引放在每次模型调用的计费记录上
ALTER TABLE agent_model_calls ADD COLUMN span_id varchar(32) NOT NULL DEFAULT '';
ALTER TABLE agent_model_calls ADD COLUMN seq bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_model_calls ADD COLUMN purpose varchar(16) NOT NULL DEFAULT '';
ALTER TABLE agent_model_calls ADD COLUMN segment_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_model_calls ADD COLUMN first_token_ms bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_model_calls ADD COLUMN duration_ms bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_model_calls ADD COLUMN finish_reason varchar(32) NOT NULL DEFAULT '';
ALTER TABLE agent_model_calls ADD COLUMN upstream_model varchar(128) NOT NULL DEFAULT '';
ALTER TABLE agent_model_calls ADD COLUMN channel_id varchar(64) NOT NULL DEFAULT '';

CREATE INDEX idx_agent_model_calls_span_id ON agent_model_calls USING btree (span_id);

-- 工具调用和运行片段的索引。不用外键：运行不删除，清理任务只删这张表的过期行。
CREATE TABLE agent_trace_spans (
    id bigserial PRIMARY KEY,
    run_id bigint NOT NULL,
    seq bigint NOT NULL,
    span_id varchar(32) NOT NULL,
    parent_span_id varchar(32) NOT NULL DEFAULT '',
    kind varchar(16) NOT NULL,
    name varchar(64) NOT NULL DEFAULT '',
    tool_call_id varchar(64) NOT NULL DEFAULT '',
    status varchar(16) NOT NULL,
    error_code varchar(32) NOT NULL DEFAULT '',
    summary varchar(255) NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0,
    bytes bigint NOT NULL DEFAULT 0,
    mutation_id bigint,
    segment_seq bigint NOT NULL DEFAULT 0,
    storage_id bigint NOT NULL DEFAULT 0,
    object_key varchar(255) NOT NULL DEFAULT '',
    started_at timestamp with time zone NOT NULL,
    ended_at timestamp with time zone
);

-- 按运行取时间线
CREATE INDEX idx_agent_trace_spans_run ON agent_trace_spans USING btree (run_id, seq);

CREATE UNIQUE INDEX uk_agent_trace_spans_span ON agent_trace_spans USING btree (run_id, span_id);

-- 清理任务按 started_at 找过期行
CREATE INDEX idx_agent_trace_spans_started_at ON agent_trace_spans USING btree (started_at);
