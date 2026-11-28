ALTER TABLE storyboard_split_jobs
    ADD COLUMN collaboration_epoch bigint NOT NULL DEFAULT 0,
    ADD COLUMN collaboration_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN mode varchar(8) NOT NULL DEFAULT '';
