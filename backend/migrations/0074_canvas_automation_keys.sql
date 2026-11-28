CREATE TABLE canvas_automation_keys (
    id bigserial PRIMARY KEY,
    canvas_id bigint NOT NULL,
    user_id bigint NOT NULL,
    epoch bigint NOT NULL,
    scope varchar(16) NOT NULL,
    token_version bigint NOT NULL,
    token_hash varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX idx_canvas_automation_keys_token_hash ON canvas_automation_keys (token_hash);
CREATE INDEX idx_canvas_automation_member ON canvas_automation_keys (canvas_id, user_id);

ALTER TABLE canvas_collaboration_events ADD COLUMN source varchar(16) NOT NULL DEFAULT '';

CREATE FUNCTION revoke_canvas_member_automation_keys() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.role = 'editor' AND (TG_OP = 'DELETE' OR NEW.role <> 'editor') THEN
        UPDATE canvas_automation_keys SET revoked_at = clock_timestamp()
        WHERE canvas_id = OLD.canvas_id AND user_id = OLD.user_id AND revoked_at IS NULL;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER revoke_canvas_member_automation_keys
AFTER DELETE OR UPDATE OF role ON canvas_viewers FOR EACH ROW
EXECUTE FUNCTION revoke_canvas_member_automation_keys();
