CREATE TABLE agent_admin_attempts (
    run_id bigint NOT NULL,
    kind varchar(32) NOT NULL,
    target varchar(128) NOT NULL,
    token varchar(64) NOT NULL,
    attempt bigint NOT NULL,
    PRIMARY KEY (run_id, kind, target, token)
);
