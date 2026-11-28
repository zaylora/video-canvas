ALTER TABLE canvas_viewers ADD COLUMN can_copy_to_library boolean NOT NULL DEFAULT false;
ALTER TABLE asset_library_items ADD COLUMN canvas_source_asset_id bigint;
CREATE UNIQUE INDEX uk_asset_library_canvas_source ON asset_library_items (user_id, canvas_source_asset_id);
