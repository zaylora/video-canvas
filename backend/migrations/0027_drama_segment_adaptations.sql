ALTER TABLE drama_segments
    ADD COLUMN adaptations jsonb NOT NULL DEFAULT '{}'::jsonb;
