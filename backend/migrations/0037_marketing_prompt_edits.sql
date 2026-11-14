-- 营销出片提示词可手改：脚本按提示词格式存用户手改的原稿。
ALTER TABLE marketing_scripts ADD COLUMN prompt_edits jsonb NOT NULL DEFAULT '{}'::jsonb;
