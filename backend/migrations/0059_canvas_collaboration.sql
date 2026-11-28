ALTER TABLE canvas_projects
    ADD COLUMN collaboration_active boolean NOT NULL DEFAULT false,
    ADD COLUMN collaboration_epoch bigint NOT NULL DEFAULT 0;

ALTER TABLE canvas_viewers ADD COLUMN role varchar(8) NOT NULL DEFAULT 'viewer',
    ADD CONSTRAINT chk_canvas_viewers_role CHECK (role IN ('viewer', 'editor'));

CREATE TABLE canvas_collaboration_records (
    canvas_id bigint NOT NULL,
    kind varchar(8) NOT NULL,
    record_id varchar(128) NOT NULL,
    ordinal bigint NOT NULL,
    body jsonb NOT NULL,
    field_versions jsonb NOT NULL DEFAULT '{}',
    revision bigint NOT NULL,
    deleted boolean NOT NULL DEFAULT false,
    PRIMARY KEY (canvas_id, kind, record_id),
    CONSTRAINT fk_canvas_collaboration_records_canvas FOREIGN KEY (canvas_id) REFERENCES canvas_projects(id) ON DELETE CASCADE
);

CREATE TABLE canvas_collaboration_receipts (
    canvas_id bigint NOT NULL,
    actor_id bigint NOT NULL,
    op_id varchar(64) NOT NULL,
    request_hash varchar(64) NOT NULL,
    result jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (canvas_id, actor_id, op_id),
    CONSTRAINT fk_canvas_collaboration_receipts_canvas FOREIGN KEY (canvas_id) REFERENCES canvas_projects(id) ON DELETE CASCADE
);

CREATE TABLE canvas_collaboration_events (
    canvas_id bigint NOT NULL,
    revision bigint NOT NULL,
    epoch bigint NOT NULL,
    actor_id bigint NOT NULL,
    op_id varchar(64) NOT NULL,
    changes jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (canvas_id, revision),
    CONSTRAINT fk_canvas_collaboration_events_canvas FOREIGN KEY (canvas_id) REFERENCES canvas_projects(id) ON DELETE CASCADE
);
