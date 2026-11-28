CREATE TABLE canvas_collaboration_checkpoints (
    canvas_id bigint NOT NULL,
    epoch bigint NOT NULL,
    revision bigint NOT NULL,
    payload_json jsonb NOT NULL,
    baseline boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (canvas_id, epoch, revision),
    CONSTRAINT fk_canvas_collaboration_checkpoints_canvas FOREIGN KEY (canvas_id) REFERENCES canvas_projects(id) ON DELETE CASCADE
);
