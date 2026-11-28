CREATE TABLE canvas_media_drafts (
    canvas_id bigint NOT NULL,
    actor_id bigint NOT NULL,
    draft_id varchar(64) NOT NULL,
    epoch bigint NOT NULL,
    revision bigint NOT NULL,
    source_node varchar(128) NOT NULL,
    source_asset bigint NOT NULL,
    state varchar(16) NOT NULL,
    storage_id bigint NOT NULL,
    storage_key varchar(512) NOT NULL,
    sha256 varchar(64) NOT NULL,
    metadata jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (canvas_id, actor_id, draft_id)
);

CREATE INDEX idx_canvas_media_drafts_expires_at ON canvas_media_drafts (expires_at);
