-- 首页顶部公告（设计：docs/design/公告/公告.md）。全站共享，不带 user_id；本期只有「通知」一种类型，不存类型列。
CREATE TABLE announcements (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    deleted_at timestamptz,
    title varchar(255) NOT NULL,
    body text NOT NULL DEFAULT '',
    link_url varchar(512) NOT NULL DEFAULT '',
    important boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL,
    starts_at timestamptz,
    ends_at timestamptz,
    sort bigint NOT NULL DEFAULT 0,
    created_by bigint NOT NULL DEFAULT 0,
    updated_by bigint NOT NULL DEFAULT 0
);
CREATE INDEX idx_announcements_deleted_at ON announcements (deleted_at);
CREATE INDEX idx_announcements_sort ON announcements (sort);
