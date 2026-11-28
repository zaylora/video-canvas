ALTER TABLE generation_tasks
    ADD COLUMN collaboration_epoch bigint NOT NULL DEFAULT 0,
    ADD COLUMN collaboration_input_revision bigint NOT NULL DEFAULT 0;
