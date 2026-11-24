-- 画布成片合成任务：按给定顺序把若干已生成视频拼成一个 mp4。
-- 提交后立刻返回，worker 在后台用 ffmpeg 转码拼接，结果回写到分镜表所在节点的 finalAssetId。
-- 同画布同节点同时只允许一条非终态任务（部分唯一索引）。
CREATE TABLE canvas_compose_jobs (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    node_id varchar(64) NOT NULL,
    mode varchar(16) NOT NULL DEFAULT '',
    status varchar(16) NOT NULL DEFAULT 'queued',
    input_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    fingerprint varchar(64) NOT NULL DEFAULT '',
    result_asset_id bigint,
    duration_ms bigint NOT NULL DEFAULT 0,
    segment_count bigint NOT NULL DEFAULT 0,
    error text NOT NULL DEFAULT '',
    attempts bigint NOT NULL DEFAULT 0,
    created_at timestamptz,
    updated_at timestamptz
);
CREATE INDEX idx_canvas_compose_user ON canvas_compose_jobs (user_id);
CREATE UNIQUE INDEX uk_canvas_compose_active ON canvas_compose_jobs (canvas_id, node_id) WHERE status IN ('queued', 'running');
