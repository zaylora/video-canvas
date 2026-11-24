-- 画布 MCP 接入：外部 AI 工具通过标准协议读写画布。
-- 一个「项目」= 一次接入 = 一个画布上下文：画布工具没有 canvas 参数，全部作用在会话绑定的画布上，
-- 所以项目直接绑定一个画布，想接多个画布就建多个项目。归属用户决定画布与积分算在谁名下。
-- 令牌挂在项目下，库里只存 sha256，明文只在签发时返回一次；停用项目立刻让全部令牌失效。
CREATE TABLE mcp_projects (
    id bigserial PRIMARY KEY,
    name varchar(64) NOT NULL,
    owner_user_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    scopes jsonb NOT NULL DEFAULT '[]'::jsonb,
    enabled boolean NOT NULL DEFAULT true,
    created_by bigint NOT NULL,
    created_at timestamptz,
    updated_at timestamptz
);
CREATE INDEX idx_mcp_projects_owner ON mcp_projects (owner_user_id);
CREATE INDEX idx_mcp_projects_canvas ON mcp_projects (canvas_id);

CREATE TABLE mcp_project_tokens (
    id bigserial PRIMARY KEY,
    project_id bigint NOT NULL,
    name varchar(64) NOT NULL,
    prefix varchar(12) NOT NULL,
    token_hash varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz
);
CREATE UNIQUE INDEX uk_mcp_project_tokens_hash ON mcp_project_tokens (token_hash);
CREATE INDEX idx_mcp_project_tokens_project ON mcp_project_tokens (project_id);
