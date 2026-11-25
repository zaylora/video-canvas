CREATE TABLE IF NOT EXISTS storage_hybrid_policy (
    id bigint PRIMARY KEY,
    state varchar(16) NOT NULL,
    cloud_id bigint NOT NULL,
    local_id bigint NOT NULL,
    limit_bytes bigint NOT NULL,
    error text NOT NULL,
    lease_token varchar(36) NOT NULL,
    lease_until timestamptz NOT NULL
);
