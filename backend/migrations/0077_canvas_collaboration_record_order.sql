CREATE INDEX idx_canvas_collab_records_live_order
    ON canvas_collaboration_records (canvas_id, ordinal, kind, record_id)
    WHERE deleted = false;
