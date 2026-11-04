-- Expand 阶段：保留 source_text，新增单元存储与项目提示词快照。
CREATE TABLE drama_source_drafts (
    id varchar(36) PRIMARY KEY,
    user_id bigint NOT NULL,
    filename varchar(255) NOT NULL,
    text text NOT NULL,
    units_json jsonb NOT NULL,
    kind_guess varchar(16) NOT NULL,
    signals_json jsonb NOT NULL,
    chars bigint NOT NULL,
    sha256 varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz
);
CREATE INDEX idx_drama_source_drafts_user_id ON drama_source_drafts (user_id);
CREATE INDEX idx_drama_source_drafts_expires_at ON drama_source_drafts (expires_at);

CREATE TABLE drama_units (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    drama_id bigint NOT NULL,
    no bigint NOT NULL,
    kind varchar(16) NOT NULL,
    title varchar(255) NOT NULL,
    text text NOT NULL,
    chars bigint NOT NULL
);
CREATE INDEX idx_drama_units_user_id ON drama_units (user_id);
CREATE UNIQUE INDEX uk_drama_unit_no ON drama_units (drama_id, no);

ALTER TABLE drama_projects
    ADD COLUMN source_chars bigint NOT NULL DEFAULT 0,
    ADD COLUMN source_sha256 varchar(64) NOT NULL DEFAULT '',
    ADD COLUMN source_filename varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN units_total bigint NOT NULL DEFAULT 0,
    ADD COLUMN unit_kind varchar(16) NOT NULL DEFAULT '',
    ADD COLUMN phase varchar(16) NOT NULL DEFAULT 'done',
    ADD COLUMN plan_cursor bigint NOT NULL DEFAULT 0,
    ADD COLUMN range_end bigint NOT NULL DEFAULT 0,
    ADD COLUMN pause_reason varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN pause_code varchar(16) NOT NULL DEFAULT '',
    ADD COLUMN prompt_settings jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE drama_episodes
    ADD COLUMN unit_from bigint NOT NULL DEFAULT 0,
    ADD COLUMN unit_to bigint NOT NULL DEFAULT 0,
    ADD COLUMN hook text NOT NULL DEFAULT '',
    ADD COLUMN cliffhanger text NOT NULL DEFAULT '';

CREATE TABLE drama_prompts (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    step varchar(16) NOT NULL,
    name varchar(40) NOT NULL,
    bodies jsonb NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz,
    updated_at timestamptz
);
CREATE UNIQUE INDEX uk_drama_prompt_name ON drama_prompts (user_id, step, name);

ALTER TABLE agent_skills ADD COLUMN drama_steps jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE drama_jobs
    ADD COLUMN lane varchar(16) NOT NULL DEFAULT 'user',
    ADD COLUMN batch_from bigint NOT NULL DEFAULT 0,
    ADD COLUMN batch_to bigint NOT NULL DEFAULT 0;
DROP INDEX uk_drama_active_job;
-- 项目行锁负责所有 lane 和媒体集号的并发判定，旧的全局唯一闸门不再适用。
UPDATE drama_jobs SET lane = 'pipeline' WHERE step IN ('analyze', 'outline');

-- 老项目按分集标记搬运；无标记作品整段保留为一个单元，不丢序言和正文。
WITH pieces AS (
    SELECT p.id, p.user_id, part.ordinality AS no, btrim(part.text) AS text,
        CASE WHEN part.text ~ '^第[零〇一二三四五六七八九十百千万两0-9]+集' THEN 'episode' ELSE 'chunk' END AS kind
    FROM drama_projects p
    CROSS JOIN LATERAL regexp_split_to_table(
        replace(p.source_text, E'\r\n', E'\n'),
        E'(?m)(?=^第[零〇一二三四五六七八九十百千万两0-9]+集)'
    ) WITH ORDINALITY AS part(text, ordinality)
    WHERE btrim(part.text) <> ''
), numbered AS (
    SELECT *, row_number() OVER (PARTITION BY id ORDER BY no) AS unit_no FROM pieces
)
INSERT INTO drama_units (user_id, drama_id, no, kind, title, text, chars)
SELECT user_id, id, unit_no, kind, left(split_part(text, E'\n', 1), 255), text, char_length(text) FROM numbered;

UPDATE drama_projects p SET source_chars = char_length(btrim(source_text)),
    source_sha256 = encode(sha256(convert_to(source_text, 'UTF8')), 'hex'),
    units_total = (SELECT count(*) FROM drama_units u WHERE u.drama_id = p.id),
    range_end = (SELECT count(*) FROM drama_units u WHERE u.drama_id = p.id),
    unit_kind = COALESCE((SELECT kind FROM drama_units u WHERE u.drama_id = p.id ORDER BY no LIMIT 1), '');
