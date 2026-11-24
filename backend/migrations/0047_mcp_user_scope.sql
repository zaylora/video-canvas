-- MCP 接入的权限单位从「画布」改成「用户」。
--
-- 令牌代表「某个用户授权的外部应用」，能访问这个用户名下的全部画布；
-- 具体操作哪一块由每次调用的 canvas 参数决定，服务端再校验归属。
-- 因此项目上的 canvas_id 只剩「默认画布」的含义（调用没指定时用它），可以留空。
ALTER TABLE mcp_projects ALTER COLUMN canvas_id DROP NOT NULL;

COMMENT ON COLUMN mcp_projects.canvas_id IS '默认画布（可空）：调用未指定 canvas 时使用，不构成权限边界';
