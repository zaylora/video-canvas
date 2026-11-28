CREATE TABLE canvas_collaboration_comments (
    id bigserial PRIMARY KEY,
    canvas_id bigint NOT NULL,
    epoch bigint NOT NULL,
    node_id varchar(128) NOT NULL,
    actor_id bigint NOT NULL,
    op_id varchar(64) NOT NULL,
    body text NOT NULL,
    mentions jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT fk_canvas_collaboration_comments_canvas FOREIGN KEY (canvas_id) REFERENCES canvas_projects(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_canvas_collab_comment_request ON canvas_collaboration_comments (canvas_id, epoch, actor_id, op_id);
CREATE INDEX idx_canvas_collab_comment_thread ON canvas_collaboration_comments (canvas_id, epoch, node_id, id);
