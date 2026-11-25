-- 每代对象有独立的 key。先登记容量预留，再做 S3 操作。
CREATE TABLE IF NOT EXISTS reference_cache_objects (
    key varchar(512) PRIMARY KEY,
    namespace varchar(64) NOT NULL,
    identity varchar(64) NOT NULL,
    digest varchar(64) NOT NULL,
    size bigint NOT NULL,
    ready boolean NOT NULL,
    deleting boolean NOT NULL,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    busy_until timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reference_cache_identity ON reference_cache_objects (namespace, identity);
CREATE INDEX IF NOT EXISTS idx_reference_cache_sweep ON reference_cache_objects (namespace, busy_until);

CREATE TABLE IF NOT EXISTS reference_cache_uses (
    object_key varchar(512) NOT NULL,
    task_id bigint NOT NULL,
    protect_until timestamptz NOT NULL,
    PRIMARY KEY (object_key, task_id)
);
