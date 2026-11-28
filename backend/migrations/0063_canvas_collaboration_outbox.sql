ALTER TABLE canvas_collaboration_events ADD COLUMN published_at timestamptz;

CREATE INDEX idx_canvas_collaboration_events_pending
    ON canvas_collaboration_events (created_at) WHERE published_at IS NULL;
