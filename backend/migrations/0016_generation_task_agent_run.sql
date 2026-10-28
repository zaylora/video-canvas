-- 生成任务记下发起它的 Agent 运行：任务结束时据此通知 Agent（0 表示不是 Agent 发起的）。
-- 只加列，带默认值，旧代码照常读写；部分索引只覆盖 Agent 发起的任务，不影响其它任务的写入。
ALTER TABLE generation_tasks ADD COLUMN agent_run_id bigint NOT NULL DEFAULT 0;
CREATE INDEX idx_generation_tasks_agent_run ON generation_tasks (agent_run_id) WHERE agent_run_id <> 0;
