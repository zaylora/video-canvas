-- 基线迁移：v0.3.2 的完整库结构（接入版本化迁移之前的最后一个版本）。
-- 生成方式：在空的 PostgreSQL 16 上按 v0.3.2 的启动顺序（旧 Migrate* → AutoMigrate → MigrateModelSingleConfig → idx_task_poll）建表，
-- 再用 pg_dump --schema-only --no-owner --no-privileges --no-comments 导出，去掉 SET 语句和 public. 前缀；
-- 两个带 IN 列表的部分索引（idx_task_poll、uk_agent_runs_active）手工改回了代码里的写法，见各自上方的注释。
-- 已经由 AutoMigrate 建好的老库不会执行这个文件，而是由 internal/migration/legacy.go 补齐后只记录它。
-- 已发布，不要修改：结构变更请新增迁移文件。

CREATE TABLE admin_audit_logs (
    id bigint NOT NULL,
    actor_id bigint DEFAULT 0 NOT NULL,
    action character varying(64) NOT NULL,
    target_type character varying(32) NOT NULL,
    target_id bigint DEFAULT 0 NOT NULL,
    detail_json json,
    created_at timestamp with time zone
);

CREATE SEQUENCE admin_audit_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE admin_audit_logs_id_seq OWNED BY admin_audit_logs.id;

CREATE TABLE agent_approvals (
    id bigint NOT NULL,
    run_id bigint NOT NULL,
    session_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    user_id bigint NOT NULL,
    kind character varying(16) NOT NULL,
    tool_call_id character varying(64) DEFAULT ''::character varying NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    quote_credits bigint DEFAULT 0 NOT NULL,
    status character varying(20) NOT NULL,
    decision_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    result_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    decided_at timestamp with time zone,
    expires_at timestamp with time zone,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE agent_approvals_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_approvals_id_seq OWNED BY agent_approvals.id;

CREATE TABLE agent_events (
    id bigint NOT NULL,
    session_id bigint NOT NULL,
    run_id bigint DEFAULT 0 NOT NULL,
    seq bigint NOT NULL,
    type character varying(32) NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE agent_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_events_id_seq OWNED BY agent_events.id;

CREATE TABLE agent_model_calls (
    id bigint NOT NULL,
    run_id bigint NOT NULL,
    user_id bigint NOT NULL,
    model_key character varying(128) NOT NULL,
    status character varying(16) NOT NULL,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    cached_tokens bigint DEFAULT 0 NOT NULL,
    usage_estimated boolean DEFAULT false NOT NULL,
    credits bigint DEFAULT 0 NOT NULL,
    charged bigint DEFAULT 0 NOT NULL,
    error character varying(255) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone,
    settled_at timestamp with time zone
);

CREATE SEQUENCE agent_model_calls_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_model_calls_id_seq OWNED BY agent_model_calls.id;

CREATE TABLE agent_mutations (
    id bigint NOT NULL,
    run_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    user_id bigint NOT NULL,
    seq bigint NOT NULL,
    tool_call_id character varying(64) DEFAULT ''::character varying NOT NULL,
    kind character varying(16) NOT NULL,
    changes_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    revision_before bigint NOT NULL,
    revision_after bigint NOT NULL,
    undone_at timestamp with time zone,
    created_at timestamp with time zone
);

CREATE SEQUENCE agent_mutations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_mutations_id_seq OWNED BY agent_mutations.id;

CREATE TABLE agent_runs (
    id bigint NOT NULL,
    session_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    user_id bigint NOT NULL,
    status character varying(20) NOT NULL,
    mode character varying(16) DEFAULT 'all'::character varying NOT NULL,
    budget_credits bigint DEFAULT 0 NOT NULL,
    spent_credits bigint DEFAULT 0 NOT NULL,
    steps bigint DEFAULT 0 NOT NULL,
    max_steps bigint DEFAULT 40 NOT NULL,
    error_code character varying(64) DEFAULT ''::character varying NOT NULL,
    error_message character varying(512) DEFAULT ''::character varying NOT NULL,
    lease_until timestamp with time zone,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    ended_at timestamp with time zone
);

CREATE SEQUENCE agent_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_runs_id_seq OWNED BY agent_runs.id;

CREATE TABLE agent_sessions (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    title character varying(100) DEFAULT ''::character varying NOT NULL,
    mode character varying(16) DEFAULT 'all'::character varying NOT NULL,
    model_key character varying(128) DEFAULT ''::character varying NOT NULL,
    session_jsonl text DEFAULT ''::text NOT NULL,
    last_seq bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone
);

CREATE SEQUENCE agent_sessions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_sessions_id_seq OWNED BY agent_sessions.id;

CREATE TABLE agent_skill_imports (
    id character varying(36) NOT NULL,
    user_id bigint NOT NULL,
    result_json jsonb NOT NULL,
    sha256 character varying(64) NOT NULL,
    storage_id bigint DEFAULT 0 NOT NULL,
    package_key character varying(255) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone
);

CREATE TABLE agent_skill_versions (
    id bigint NOT NULL,
    skill_id bigint NOT NULL,
    version bigint NOT NULL,
    state character varying(16) NOT NULL,
    sha256 character varying(64) NOT NULL,
    description character varying(1024) NOT NULL,
    frontmatter_json jsonb,
    unsupported_fields jsonb,
    body_text text NOT NULL,
    files_json jsonb,
    file_count bigint DEFAULT 0 NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    has_scripts boolean DEFAULT false NOT NULL,
    issues_json jsonb,
    storage_id bigint DEFAULT 0 NOT NULL,
    package_key character varying(255) NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE agent_skill_versions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_skill_versions_id_seq OWNED BY agent_skill_versions.id;

CREATE TABLE agent_skills (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    title character varying(128) NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    active_version bigint,
    latest_version bigint DEFAULT 0 NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE agent_skills_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_skills_id_seq OWNED BY agent_skills.id;

CREATE TABLE ai_audit_logs (
    id bigint NOT NULL,
    actor_id bigint DEFAULT 0 NOT NULL,
    action character varying(64) NOT NULL,
    target_type character varying(32) NOT NULL,
    target_key character varying(128) NOT NULL,
    detail_json json,
    created_at timestamp with time zone
);

CREATE SEQUENCE ai_audit_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE ai_audit_logs_id_seq OWNED BY ai_audit_logs.id;

CREATE TABLE ai_channels (
    key character varying(64) NOT NULL,
    name character varying(128) NOT NULL,
    plugin_key character varying(30) NOT NULL,
    plugin_version_id bigint NOT NULL,
    base_url character varying(512) NOT NULL,
    allow_credentials boolean DEFAULT false NOT NULL,
    settings_json json NOT NULL,
    rate_limit_json json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE TABLE ai_config_revisions (
    id bigint NOT NULL,
    target character varying(16) NOT NULL,
    target_key character varying(128) NOT NULL,
    revision_no bigint NOT NULL,
    body_json json NOT NULL,
    status character varying(16) NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    note character varying(255) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE ai_config_revisions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE ai_config_revisions_id_seq OWNED BY ai_config_revisions.id;

CREATE TABLE ai_models (
    key character varying(128) NOT NULL,
    kind character varying(16) NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    sort bigint DEFAULT 100 NOT NULL,
    published_revision_id bigint,
    updated_at timestamp with time zone
);

CREATE TABLE ai_plugin_versions (
    id bigint NOT NULL,
    plugin_key character varying(30) NOT NULL,
    version character varying(32) NOT NULL,
    sha256 character varying(64) NOT NULL,
    code text NOT NULL,
    meta_json json NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE ai_plugin_versions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE ai_plugin_versions_id_seq OWNED BY ai_plugin_versions.id;

CREATE TABLE ai_plugins (
    key character varying(30) NOT NULL,
    name character varying(128) NOT NULL,
    source character varying(16) DEFAULT 'uploaded'::character varying NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE ai_secrets (
    name character varying(128) NOT NULL,
    ciphertext bytea NOT NULL,
    nonce bytea NOT NULL,
    key_version bigint DEFAULT 1 NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE asset_upload_intents (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    storage_id bigint NOT NULL,
    storage_key character varying(512) NOT NULL,
    file_name character varying(255) DEFAULT ''::character varying NOT NULL,
    mime_type character varying(128) DEFAULT ''::character varying NOT NULL,
    kind character varying(16) DEFAULT ''::character varying NOT NULL,
    declared_size bigint DEFAULT 0 NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone
);

CREATE SEQUENCE asset_upload_intents_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE asset_upload_intents_id_seq OWNED BY asset_upload_intents.id;

CREATE TABLE assets (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    kind character varying(16) NOT NULL,
    storage_id bigint DEFAULT 0 NOT NULL,
    storage_key character varying(512) NOT NULL,
    mime_type character varying(128) NOT NULL,
    byte_size bigint DEFAULT 0 NOT NULL,
    width bigint,
    height bigint,
    duration_ms bigint,
    source character varying(16) NOT NULL,
    task_id bigint,
    file_name character varying(255) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE assets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE assets_id_seq OWNED BY assets.id;

CREATE TABLE canvas_projects (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    user_id bigint NOT NULL,
    title character varying(255) DEFAULT ''::character varying NOT NULL,
    payload_json jsonb NOT NULL,
    revision bigint DEFAULT 1 NOT NULL
);

CREATE SEQUENCE canvas_projects_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE canvas_projects_id_seq OWNED BY canvas_projects.id;

CREATE TABLE conversation_records (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    conversation_id bigint NOT NULL,
    user_id bigint NOT NULL,
    kind character varying(16) NOT NULL,
    model_id character varying(128) NOT NULL,
    prompt text DEFAULT ''::text NOT NULL,
    input_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    count bigint DEFAULT 1 NOT NULL,
    task_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    submit_errors jsonb DEFAULT '[]'::jsonb NOT NULL,
    quote_credits bigint DEFAULT 0 NOT NULL,
    idempotency_key character varying(128) DEFAULT ''::character varying NOT NULL
);

CREATE SEQUENCE conversation_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE conversation_records_id_seq OWNED BY conversation_records.id;

CREATE TABLE conversations (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    user_id bigint NOT NULL,
    title character varying(50) DEFAULT ''::character varying NOT NULL,
    record_count bigint DEFAULT 0 NOT NULL,
    last_record_at timestamp with time zone
);

CREATE SEQUENCE conversations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE conversations_id_seq OWNED BY conversations.id;

CREATE TABLE credit_ledger (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    task_id bigint,
    agent_call_id bigint,
    type character varying(16) NOT NULL,
    amount bigint NOT NULL,
    operator_id bigint,
    note text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE credit_ledger_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE credit_ledger_id_seq OWNED BY credit_ledger.id;

CREATE TABLE generation_tasks (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    canvas_project_id bigint,
    node_id character varying(64) DEFAULT ''::character varying NOT NULL,
    kind character varying(16) NOT NULL,
    model_id character varying(128) NOT NULL,
    task_ref character varying(32) DEFAULT ''::character varying NOT NULL,
    provider character varying(64) NOT NULL,
    provider_task_id character varying(128) DEFAULT ''::character varying NOT NULL,
    status character varying(16) NOT NULL,
    progress bigint,
    input_json jsonb NOT NULL,
    output_json jsonb,
    error_code character varying(64) DEFAULT ''::character varying NOT NULL,
    error_message character varying(512) DEFAULT ''::character varying NOT NULL,
    credits bigint DEFAULT 0 NOT NULL,
    charged_credits bigint,
    version bigint DEFAULT 1 NOT NULL,
    idempotency_key character varying(128) DEFAULT ''::character varying NOT NULL,
    config_snapshot jsonb NOT NULL,
    provider_state jsonb,
    provider_result jsonb,
    trace_json jsonb,
    is_test boolean DEFAULT false NOT NULL,
    next_poll_at timestamp with time zone NOT NULL,
    poll_attempts bigint DEFAULT 0 NOT NULL,
    lease_until timestamp with time zone,
    deadline_at timestamp with time zone NOT NULL,
    submitted_at timestamp with time zone,
    finished_at timestamp with time zone,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE generation_tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE generation_tasks_id_seq OWNED BY generation_tasks.id;

CREATE TABLE image_processor_versions (
    id bigint NOT NULL,
    processor_id bigint NOT NULL,
    version bigint NOT NULL,
    config json NOT NULL,
    trial_result json,
    published_by bigint DEFAULT 0 NOT NULL,
    published_at timestamp with time zone
);

CREATE SEQUENCE image_processor_versions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE image_processor_versions_id_seq OWNED BY image_processor_versions.id;

CREATE TABLE image_processors (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    vendor character varying(24) NOT NULL,
    storage_id bigint NOT NULL,
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    config json NOT NULL,
    published_config json,
    published_version bigint DEFAULT 0 NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    check_result json,
    created_by bigint DEFAULT 0 NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE image_processors_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE image_processors_id_seq OWNED BY image_processors.id;

CREATE TABLE showcase_items (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    asset_id bigint NOT NULL,
    poster_asset_id bigint,
    prompt character varying(255) NOT NULL,
    model_label character varying(80) DEFAULT ''::character varying NOT NULL,
    start_sec numeric DEFAULT 0 NOT NULL,
    enabled boolean NOT NULL,
    sort bigint DEFAULT 0 NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE showcase_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE showcase_items_id_seq OWNED BY showcase_items.id;

CREATE TABLE smtp_settings (
    id bigint NOT NULL,
    host character varying(255) DEFAULT ''::character varying NOT NULL,
    port bigint DEFAULT 0 NOT NULL,
    encryption character varying(16) DEFAULT 'starttls'::character varying NOT NULL,
    username character varying(255) DEFAULT ''::character varying NOT NULL,
    password_enc bytea,
    password_nonce bytea,
    from_address character varying(255) DEFAULT ''::character varying NOT NULL,
    from_name character varying(128) DEFAULT ''::character varying NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    last_check_at timestamp with time zone,
    last_check_ok boolean,
    last_check_error text DEFAULT ''::text NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone
);

CREATE SEQUENCE smtp_settings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE smtp_settings_id_seq OWNED BY smtp_settings.id;

CREATE TABLE storage_configs (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    provider character varying(16) NOT NULL,
    builtin boolean DEFAULT false NOT NULL,
    account_id character varying(64) DEFAULT ''::character varying NOT NULL,
    endpoint character varying(255) DEFAULT ''::character varying NOT NULL,
    region character varying(64) DEFAULT ''::character varying NOT NULL,
    bucket character varying(128) DEFAULT ''::character varying NOT NULL,
    path_prefix character varying(255) DEFAULT ''::character varying NOT NULL,
    addressing character varying(8) DEFAULT 'auto'::character varying NOT NULL,
    use_ssl boolean DEFAULT true NOT NULL,
    access_key_id character varying(128) DEFAULT ''::character varying NOT NULL,
    public_base_url character varying(512) DEFAULT ''::character varying NOT NULL,
    signed_ttl_sec bigint DEFAULT 3600 NOT NULL,
    direct_upload boolean DEFAULT false NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    last_check_at timestamp with time zone,
    last_check_ok boolean,
    last_check_error text DEFAULT ''::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE storage_configs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE storage_configs_id_seq OWNED BY storage_configs.id;

CREATE TABLE system_settings (
    key character varying(64) NOT NULL,
    value text NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE user_credits (
    user_id bigint NOT NULL,
    balance bigint DEFAULT 0 NOT NULL,
    frozen bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone
);

CREATE SEQUENCE user_credits_user_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE user_credits_user_id_seq OWNED BY user_credits.user_id;

CREATE TABLE user_login_logs (
    id bigint NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL,
    kind character varying(16) NOT NULL,
    result character varying(16) NOT NULL,
    ip character varying(64) DEFAULT ''::character varying NOT NULL,
    user_agent character varying(255) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE user_login_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE user_login_logs_id_seq OWNED BY user_login_logs.id;

CREATE TABLE users (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    username character varying(64) NOT NULL,
    password character varying(128) NOT NULL,
    nickname character varying(64),
    email character varying(128),
    role character varying(16) DEFAULT 'user'::character varying NOT NULL,
    status character varying(16) DEFAULT 'active'::character varying NOT NULL,
    max_active_tasks bigint,
    email_verified_at timestamp with time zone,
    last_login_at timestamp with time zone,
    token_version bigint DEFAULT 0 NOT NULL,
    avatar_key character varying(255) DEFAULT ''::character varying NOT NULL,
    avatar_storage_id bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE users_id_seq OWNED BY users.id;

ALTER TABLE ONLY admin_audit_logs ALTER COLUMN id SET DEFAULT nextval('admin_audit_logs_id_seq'::regclass);

ALTER TABLE ONLY agent_approvals ALTER COLUMN id SET DEFAULT nextval('agent_approvals_id_seq'::regclass);

ALTER TABLE ONLY agent_events ALTER COLUMN id SET DEFAULT nextval('agent_events_id_seq'::regclass);

ALTER TABLE ONLY agent_model_calls ALTER COLUMN id SET DEFAULT nextval('agent_model_calls_id_seq'::regclass);

ALTER TABLE ONLY agent_mutations ALTER COLUMN id SET DEFAULT nextval('agent_mutations_id_seq'::regclass);

ALTER TABLE ONLY agent_runs ALTER COLUMN id SET DEFAULT nextval('agent_runs_id_seq'::regclass);

ALTER TABLE ONLY agent_sessions ALTER COLUMN id SET DEFAULT nextval('agent_sessions_id_seq'::regclass);

ALTER TABLE ONLY agent_skill_versions ALTER COLUMN id SET DEFAULT nextval('agent_skill_versions_id_seq'::regclass);

ALTER TABLE ONLY agent_skills ALTER COLUMN id SET DEFAULT nextval('agent_skills_id_seq'::regclass);

ALTER TABLE ONLY ai_audit_logs ALTER COLUMN id SET DEFAULT nextval('ai_audit_logs_id_seq'::regclass);

ALTER TABLE ONLY ai_config_revisions ALTER COLUMN id SET DEFAULT nextval('ai_config_revisions_id_seq'::regclass);

ALTER TABLE ONLY ai_plugin_versions ALTER COLUMN id SET DEFAULT nextval('ai_plugin_versions_id_seq'::regclass);

ALTER TABLE ONLY asset_upload_intents ALTER COLUMN id SET DEFAULT nextval('asset_upload_intents_id_seq'::regclass);

ALTER TABLE ONLY assets ALTER COLUMN id SET DEFAULT nextval('assets_id_seq'::regclass);

ALTER TABLE ONLY canvas_projects ALTER COLUMN id SET DEFAULT nextval('canvas_projects_id_seq'::regclass);

ALTER TABLE ONLY conversation_records ALTER COLUMN id SET DEFAULT nextval('conversation_records_id_seq'::regclass);

ALTER TABLE ONLY conversations ALTER COLUMN id SET DEFAULT nextval('conversations_id_seq'::regclass);

ALTER TABLE ONLY credit_ledger ALTER COLUMN id SET DEFAULT nextval('credit_ledger_id_seq'::regclass);

ALTER TABLE ONLY generation_tasks ALTER COLUMN id SET DEFAULT nextval('generation_tasks_id_seq'::regclass);

ALTER TABLE ONLY image_processor_versions ALTER COLUMN id SET DEFAULT nextval('image_processor_versions_id_seq'::regclass);

ALTER TABLE ONLY image_processors ALTER COLUMN id SET DEFAULT nextval('image_processors_id_seq'::regclass);

ALTER TABLE ONLY showcase_items ALTER COLUMN id SET DEFAULT nextval('showcase_items_id_seq'::regclass);

ALTER TABLE ONLY smtp_settings ALTER COLUMN id SET DEFAULT nextval('smtp_settings_id_seq'::regclass);

ALTER TABLE ONLY storage_configs ALTER COLUMN id SET DEFAULT nextval('storage_configs_id_seq'::regclass);

ALTER TABLE ONLY user_credits ALTER COLUMN user_id SET DEFAULT nextval('user_credits_user_id_seq'::regclass);

ALTER TABLE ONLY user_login_logs ALTER COLUMN id SET DEFAULT nextval('user_login_logs_id_seq'::regclass);

ALTER TABLE ONLY users ALTER COLUMN id SET DEFAULT nextval('users_id_seq'::regclass);

ALTER TABLE ONLY admin_audit_logs
    ADD CONSTRAINT admin_audit_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_approvals
    ADD CONSTRAINT agent_approvals_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_events
    ADD CONSTRAINT agent_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_model_calls
    ADD CONSTRAINT agent_model_calls_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_mutations
    ADD CONSTRAINT agent_mutations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_runs
    ADD CONSTRAINT agent_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_sessions
    ADD CONSTRAINT agent_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_skill_imports
    ADD CONSTRAINT agent_skill_imports_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_skill_versions
    ADD CONSTRAINT agent_skill_versions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agent_skills
    ADD CONSTRAINT agent_skills_pkey PRIMARY KEY (id);

ALTER TABLE ONLY ai_audit_logs
    ADD CONSTRAINT ai_audit_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY ai_channels
    ADD CONSTRAINT ai_channels_pkey PRIMARY KEY (key);

ALTER TABLE ONLY ai_config_revisions
    ADD CONSTRAINT ai_config_revisions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY ai_models
    ADD CONSTRAINT ai_models_pkey PRIMARY KEY (key);

ALTER TABLE ONLY ai_plugin_versions
    ADD CONSTRAINT ai_plugin_versions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY ai_plugins
    ADD CONSTRAINT ai_plugins_pkey PRIMARY KEY (key);

ALTER TABLE ONLY ai_secrets
    ADD CONSTRAINT ai_secrets_pkey PRIMARY KEY (name);

ALTER TABLE ONLY asset_upload_intents
    ADD CONSTRAINT asset_upload_intents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY assets
    ADD CONSTRAINT assets_pkey PRIMARY KEY (id);

ALTER TABLE ONLY canvas_projects
    ADD CONSTRAINT canvas_projects_pkey PRIMARY KEY (id);

ALTER TABLE ONLY conversation_records
    ADD CONSTRAINT conversation_records_pkey PRIMARY KEY (id);

ALTER TABLE ONLY conversations
    ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY credit_ledger
    ADD CONSTRAINT credit_ledger_pkey PRIMARY KEY (id);

ALTER TABLE ONLY generation_tasks
    ADD CONSTRAINT generation_tasks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY image_processor_versions
    ADD CONSTRAINT image_processor_versions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY image_processors
    ADD CONSTRAINT image_processors_pkey PRIMARY KEY (id);

ALTER TABLE ONLY showcase_items
    ADD CONSTRAINT showcase_items_pkey PRIMARY KEY (id);

ALTER TABLE ONLY smtp_settings
    ADD CONSTRAINT smtp_settings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY storage_configs
    ADD CONSTRAINT storage_configs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY system_settings
    ADD CONSTRAINT system_settings_pkey PRIMARY KEY (key);

ALTER TABLE ONLY user_credits
    ADD CONSTRAINT user_credits_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY user_login_logs
    ADD CONSTRAINT user_login_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

CREATE INDEX idx_admin_audit_logs_action ON admin_audit_logs USING btree (action);

CREATE INDEX idx_admin_audit_logs_actor_id ON admin_audit_logs USING btree (actor_id);

CREATE INDEX idx_admin_audit_logs_created_at ON admin_audit_logs USING btree (created_at);

CREATE INDEX idx_admin_audit_target ON admin_audit_logs USING btree (target_type, target_id);

CREATE INDEX idx_agent_approvals_run ON agent_approvals USING btree (run_id, status);

CREATE INDEX idx_agent_approvals_user_id ON agent_approvals USING btree (user_id);

CREATE INDEX idx_agent_events_created_at ON agent_events USING btree (created_at);

CREATE INDEX idx_agent_model_calls_run_id ON agent_model_calls USING btree (run_id);

CREATE INDEX idx_agent_model_calls_user_id ON agent_model_calls USING btree (user_id);

CREATE INDEX idx_agent_mutations_canvas_id ON agent_mutations USING btree (canvas_id);

CREATE INDEX idx_agent_runs_session_id ON agent_runs USING btree (session_id);

CREATE INDEX idx_agent_runs_user_id ON agent_runs USING btree (user_id);

CREATE INDEX idx_agent_sessions_canvas ON agent_sessions USING btree (user_id, canvas_id, updated_at DESC);

CREATE INDEX idx_agent_sessions_deleted_at ON agent_sessions USING btree (deleted_at);

CREATE INDEX idx_agent_skill_imports_expires_at ON agent_skill_imports USING btree (expires_at);

CREATE INDEX idx_agent_skill_imports_user_id ON agent_skill_imports USING btree (user_id);

CREATE INDEX idx_agent_skill_versions_sha256 ON agent_skill_versions USING btree (sha256);

CREATE INDEX idx_agent_skill_versions_state ON agent_skill_versions USING btree (state);

CREATE UNIQUE INDEX idx_agent_skills_name ON agent_skills USING btree (name);

CREATE INDEX idx_ai_audit_logs_action ON ai_audit_logs USING btree (action);

CREATE INDEX idx_ai_audit_logs_actor_id ON ai_audit_logs USING btree (actor_id);

CREATE INDEX idx_ai_audit_logs_created_at ON ai_audit_logs USING btree (created_at);

CREATE INDEX idx_ai_audit_logs_target_key ON ai_audit_logs USING btree (target_key);

CREATE INDEX idx_ai_channels_plugin_key ON ai_channels USING btree (plugin_key);

CREATE INDEX idx_ai_channels_plugin_version_id ON ai_channels USING btree (plugin_version_id);

CREATE INDEX idx_ai_models_kind ON ai_models USING btree (kind);

CREATE INDEX idx_ai_plugin_versions_plugin_key ON ai_plugin_versions USING btree (plugin_key);

CREATE INDEX idx_ai_plugin_versions_sha256 ON ai_plugin_versions USING btree (sha256);

CREATE INDEX idx_asset_upload_intents_expires_at ON asset_upload_intents USING btree (expires_at);

CREATE INDEX idx_asset_upload_intents_storage_id ON asset_upload_intents USING btree (storage_id);

CREATE UNIQUE INDEX idx_asset_upload_intents_storage_key ON asset_upload_intents USING btree (storage_key);

CREATE INDEX idx_asset_upload_intents_user_id ON asset_upload_intents USING btree (user_id);

CREATE INDEX idx_assets_storage_id ON assets USING btree (storage_id);

CREATE UNIQUE INDEX idx_assets_storage_key ON assets USING btree (storage_key);

CREATE INDEX idx_assets_task_id ON assets USING btree (task_id);

CREATE INDEX idx_assets_user_id ON assets USING btree (user_id);

CREATE INDEX idx_canvas_projects_deleted_at ON canvas_projects USING btree (deleted_at);

CREATE INDEX idx_canvas_projects_user_id ON canvas_projects USING btree (user_id);

CREATE INDEX idx_conv_records_conv ON conversation_records USING btree (conversation_id);

CREATE INDEX idx_conv_records_user ON conversation_records USING btree (user_id);

CREATE INDEX idx_conversation_records_deleted_at ON conversation_records USING btree (deleted_at);

CREATE INDEX idx_conversations_deleted_at ON conversations USING btree (deleted_at);

CREATE INDEX idx_conversations_user_last ON conversations USING btree (user_id, last_record_at DESC);

CREATE INDEX idx_credit_ledger_user_id ON credit_ledger USING btree (user_id);

CREATE INDEX idx_generation_tasks_canvas_project_id ON generation_tasks USING btree (canvas_project_id);

CREATE UNIQUE INDEX idx_image_processors_name ON image_processors USING btree (name);

CREATE INDEX idx_image_processors_storage_id ON image_processors USING btree (storage_id);

CREATE INDEX idx_login_user_created ON user_login_logs USING btree (user_id, created_at DESC);

CREATE INDEX idx_showcase_items_asset_id ON showcase_items USING btree (asset_id);

CREATE INDEX idx_showcase_items_deleted_at ON showcase_items USING btree (deleted_at);

CREATE INDEX idx_showcase_items_sort ON showcase_items USING btree (sort);

CREATE UNIQUE INDEX idx_skill_version ON agent_skill_versions USING btree (skill_id, version);

CREATE UNIQUE INDEX idx_storage_configs_name ON storage_configs USING btree (name);

CREATE INDEX idx_task_created ON generation_tasks USING btree (created_at);

-- 条件写成和代码里一样的 IN 列表：pg_dump 导出的 = ANY (ARRAY...) 形式再执行一次，PostgreSQL 记录的定义会和 AutoMigrate 建出的不同
CREATE INDEX idx_task_poll ON generation_tasks USING btree (next_poll_at) WHERE status IN ('pending','queued','running','finalizing');

CREATE INDEX idx_task_provider ON generation_tasks USING btree (provider_task_id);

CREATE INDEX idx_task_user_created ON generation_tasks USING btree (user_id, created_at);

CREATE INDEX idx_task_user_status ON generation_tasks USING btree (user_id, status);

CREATE INDEX idx_users_avatar_key ON users USING btree (avatar_key) WHERE ((avatar_key)::text <> ''::text);

CREATE INDEX idx_users_deleted_at ON users USING btree (deleted_at);

CREATE UNIQUE INDEX idx_users_username ON users USING btree (username);

CREATE UNIQUE INDEX uidx_image_processor_published ON image_processors USING btree (storage_id) WHERE ((status)::text = 'published'::text);

CREATE UNIQUE INDEX uidx_image_processor_version ON image_processor_versions USING btree (processor_id, version);

CREATE UNIQUE INDEX uidx_storage_default ON storage_configs USING btree (is_default) WHERE is_default;

CREATE UNIQUE INDEX uk_agent_events_seq ON agent_events USING btree (session_id, seq);

CREATE UNIQUE INDEX uk_agent_mutations_seq ON agent_mutations USING btree (run_id, seq);

-- 同上，保持与 model.AgentRun 标签里的写法一致
CREATE UNIQUE INDEX uk_agent_runs_active ON agent_runs USING btree (canvas_id) WHERE status IN ('queued','running','waiting_approval','waiting_input');

CREATE UNIQUE INDEX uk_conv_records_idem ON conversation_records USING btree (user_id, idempotency_key) WHERE (((idempotency_key)::text <> ''::text) AND (deleted_at IS NULL));

CREATE UNIQUE INDEX uk_ledger_agent_type ON credit_ledger USING btree (agent_call_id, type) WHERE (agent_call_id IS NOT NULL);

CREATE UNIQUE INDEX uk_ledger_task_type ON credit_ledger USING btree (task_id, type) WHERE (task_id IS NOT NULL);

CREATE UNIQUE INDEX uk_plugin_version ON ai_plugin_versions USING btree (plugin_key, version);

CREATE UNIQUE INDEX uk_rev_target_no ON ai_config_revisions USING btree (target, target_key, revision_no);

CREATE UNIQUE INDEX uk_task_ref ON generation_tasks USING btree (task_ref) WHERE ((task_ref)::text <> ''::text);

CREATE UNIQUE INDEX uk_task_user_idem ON generation_tasks USING btree (user_id, idempotency_key) WHERE ((idempotency_key)::text <> ''::text);

CREATE UNIQUE INDEX uk_users_email ON users USING btree (email) WHERE ((email)::text <> ''::text);
