-- 短剧记下拆解时选的图片模型和视频模型：资产提示词和分镜提示词按所选模型的写法写，
-- 资产节点和片段默认用它们生成。空串表示没选过，提示词按通用写法。
ALTER TABLE IF EXISTS drama_projects ADD COLUMN IF NOT EXISTS image_model varchar(128) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS drama_projects ADD COLUMN IF NOT EXISTS video_model varchar(128) NOT NULL DEFAULT '';
