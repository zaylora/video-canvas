-- 二期片段只引用已有任务与素材；删除片段不删除生成结果。
-- storyboard_status 已由 0017 建立，升级时不重复添加。
CREATE TABLE drama_segments (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    deleted_at timestamptz,
    user_id bigint NOT NULL,
    episode_id bigint NOT NULL,
    seq bigint NOT NULL DEFAULT 0,
    seconds bigint NOT NULL DEFAULT 5,
    prompt text NOT NULL DEFAULT '',
    shots jsonb NOT NULL DEFAULT '[]'::jsonb,
    keyframe_asset_id bigint,
    chosen_task_id bigint,
    revision bigint NOT NULL DEFAULT 1
);
CREATE INDEX idx_drama_segments_deleted_at ON drama_segments (deleted_at);
CREATE INDEX idx_drama_segments_user_id ON drama_segments (user_id);
CREATE INDEX idx_drama_segment_episode_seq ON drama_segments (episode_id, seq);
