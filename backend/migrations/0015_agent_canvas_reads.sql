-- 已读状态属于平台的画布写入约束，持久化后跨审批续跑、进程重启和实例生效。
-- run 在独立 Agent 服务的 SQLite 中，因此不向 Go 的 agent_runs 建外键。
CREATE TABLE agent_canvas_reads (
    user_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    session_id bigint NOT NULL,
    run_id bigint NOT NULL,
    node_id text NOT NULL,
    created_at timestamptz,
    PRIMARY KEY (user_id, canvas_id, session_id, run_id, node_id)
);
