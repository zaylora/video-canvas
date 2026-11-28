CREATE TABLE canvas_compose_jobs (
    id bigserial PRIMARY KEY,
    canvas_id bigint NOT NULL,
    user_id bigint NOT NULL,
    op_id varchar(64) NOT NULL,
    epoch bigint NOT NULL,
    revision bigint NOT NULL,
    request_hash varchar(64) NOT NULL,
    inputs jsonb NOT NULL,
    status varchar(16) NOT NULL,
    error text NOT NULL DEFAULT '',
    attempt bigint NOT NULL DEFAULT 0,
    asset_id bigint NOT NULL DEFAULT 0,
    result_node varchar(128) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX idx_canvas_compose_request ON canvas_compose_jobs (canvas_id, user_id, op_id);
