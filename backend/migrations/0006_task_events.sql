-- 任务状态时间线：每次真正的状态变化记一行，设计见 docs/design/日志管理/日志管理.md。
-- 不用外键：任务本身不删除，事件只引用任务 id；与状态迁移在同一事务里写入。
CREATE TABLE generation_task_events (
    id bigserial PRIMARY KEY,
    task_id bigint NOT NULL,
    event varchar(24) NOT NULL,
    from_status varchar(16) NOT NULL DEFAULT '',
    to_status varchar(16) NOT NULL,
    detail_json jsonb,
    created_at timestamp with time zone
);

-- 按任务取时间线（id 升序即发生顺序）
CREATE INDEX idx_task_events_task ON generation_task_events USING btree (task_id, id);
