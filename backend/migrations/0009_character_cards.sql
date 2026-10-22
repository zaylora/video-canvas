-- 角色卡：用户私有的角色卡素材引用，不建立到 assets 的外键。
CREATE TABLE character_cards (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    name varchar(64) NOT NULL,
    description text NOT NULL DEFAULT '',
    avatar_asset_id bigint NOT NULL,
    extra_asset_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    voice_asset_id bigint,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

-- 同一用户下名称忽略大小写唯一。
CREATE UNIQUE INDEX uk_character_cards_user_name ON character_cards USING btree (user_id, lower(name::text));

-- 用户角色卡按更新时间倒序列出。
CREATE INDEX idx_character_cards_user_updated ON character_cards USING btree (user_id, updated_at DESC);
