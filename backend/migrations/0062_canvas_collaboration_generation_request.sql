ALTER TABLE generation_tasks
    ADD COLUMN collaboration_request_hash varchar(64) NOT NULL DEFAULT '';
