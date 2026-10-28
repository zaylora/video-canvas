ALTER TABLE drama_episodes
    ADD COLUMN segment_version bigint NOT NULL DEFAULT 0,
    ADD COLUMN final_segment_version bigint NOT NULL DEFAULT 0;

ALTER TABLE drama_jobs ADD COLUMN approval_id bigint;
CREATE UNIQUE INDEX uk_drama_job_approval ON drama_jobs (user_id, approval_id);
