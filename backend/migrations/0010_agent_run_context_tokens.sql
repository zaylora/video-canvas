-- Agent 运行记录当前的上下文大小：最近一次对话调用送进模型的输入 Token 数。
-- 画布 Agent 输入框据此显示上下文用量；刷新页面后从最近一次运行读出。
-- 只加列，带默认值，旧代码照常读写。
ALTER TABLE agent_runs ADD COLUMN context_tokens bigint NOT NULL DEFAULT 0;
