ALTER TABLE drama_projects
    ADD COLUMN video_model varchar(128) NOT NULL DEFAULT '',
    ADD COLUMN image_models jsonb NOT NULL DEFAULT '{}'::jsonb;
