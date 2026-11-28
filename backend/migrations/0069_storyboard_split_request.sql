ALTER TABLE storyboard_split_jobs
    ADD COLUMN request_key varchar(64),
    ADD COLUMN request_hash varchar(64) NOT NULL DEFAULT '',
    ADD COLUMN max_credits bigint;

CREATE UNIQUE INDEX uk_storyboard_split_request ON storyboard_split_jobs (user_id, request_key);
