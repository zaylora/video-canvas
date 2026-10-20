-- 任务调度改用 River（设计见 docs/design/任务队列River/任务队列River.md）。
-- slot_acquired_at：任务拿到“用户名额 + 渠道名额”的时间；非空且非终态表示正在占用名额，空表示还在排队等名额。
-- worker_instances：每个运行中的实例每 10 秒写一次心跳，后台“任务队列”页据此展示实例，也用来判断实例是否失联。
-- River 自己的表（river_job 等）由 rivermigrate 单独迁移，不在这里。

ALTER TABLE generation_tasks ADD COLUMN slot_acquired_at timestamp with time zone;

-- 升级前已经在上游处理的任务视为已占用名额，否则升级瞬间渠道和用户的并发数会被低估
UPDATE generation_tasks
SET slot_acquired_at = COALESCE(submitted_at, created_at, now())
WHERE status IN ('queued', 'running', 'finalizing') OR (status = 'pending' AND submitted_at IS NOT NULL);

-- 以下三个部分索引的条件带 IN 列表，GORM 标签写不出来，只在迁移里建（结构一致性测试里登记为 autoMigrateOnly）
CREATE INDEX idx_task_slot_user ON generation_tasks USING btree (user_id) WHERE slot_acquired_at IS NOT NULL AND status IN ('pending','queued','running','finalizing');

CREATE INDEX idx_task_slot_provider ON generation_tasks USING btree (provider) WHERE slot_acquired_at IS NOT NULL AND status IN ('pending','queued','running');

CREATE INDEX idx_task_waiting ON generation_tasks USING btree (provider, created_at, id) WHERE status = 'pending' AND slot_acquired_at IS NULL;

CREATE TABLE worker_instances (
    id varchar(64) PRIMARY KEY,
    hostname varchar(128) NOT NULL DEFAULT '',
    version varchar(64) NOT NULL DEFAULT '',
    started_at timestamp with time zone NOT NULL,
    heartbeat_at timestamp with time zone NOT NULL,
    is_leader boolean NOT NULL DEFAULT false,
    stats_json jsonb
);

CREATE INDEX idx_worker_instances_heartbeat ON worker_instances USING btree (heartbeat_at);
