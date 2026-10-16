-- 资产库：个人文件夹（一层）与库归属，设计见 docs/design/资产库/资产库.md。
-- 不用外键：删除文件夹时由服务在同一事务里先删归属行再删文件夹；归属只是 assets 行的引用，不复制文件。
CREATE TABLE asset_folders (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    name varchar(30) NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

-- 同一用户下文件夹名忽略大小写唯一
CREATE UNIQUE INDEX uk_asset_folders_user_name ON asset_folders USING btree (user_id, lower(name::text));

CREATE TABLE asset_library_items (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    asset_id bigint NOT NULL,
    folder_id bigint,
    created_at timestamp with time zone
);

-- 一个素材在同一用户的库里最多一条归属
CREATE UNIQUE INDEX uk_asset_library_user_asset ON asset_library_items USING btree (user_id, asset_id);

-- 库内按文件夹列出、按加入时间倒序翻页
CREATE INDEX idx_asset_library_user_folder ON asset_library_items USING btree (user_id, folder_id, created_at DESC, id DESC);

-- 生成历史：按用户 + 来源 + 类型倒序翻页
CREATE INDEX idx_assets_user_source_kind_created ON assets USING btree (user_id, source, kind, created_at DESC, id DESC);
