-- 模型调用实际发给上游的请求体（脱敏、gzip 压缩），给事后分析"模型当时看到了什么"。
-- 一次运行几十次调用、每次都带完整上下文，体量大，所以单独一张表；保留 7 天后由清理任务整行删除。
-- 超过上限的请求体只留摘要（body 为空）。新表，旧代码不受影响。
CREATE TABLE agent_model_call_bodies (
    call_id bigint PRIMARY KEY,
    run_id bigint NOT NULL,
    message_count bigint NOT NULL DEFAULT 0,
    tool_count bigint NOT NULL DEFAULT 0,
    params jsonb,
    raw_bytes bigint NOT NULL DEFAULT 0,
    body bytea,
    created_at timestamp with time zone NOT NULL
);

CREATE INDEX idx_agent_model_call_bodies_run ON agent_model_call_bodies USING btree (run_id);

CREATE INDEX idx_agent_model_call_bodies_created ON agent_model_call_bodies USING btree (created_at);
