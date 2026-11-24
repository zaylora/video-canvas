/**
 * 「我的 MCP 接入」接口路径（前缀 /api/v1 由 axios baseURL 提供）。
 * 只接受登录 JWT，作用范围永远是当前登录用户自己的项目——不需要也不允许指定归属用户。
 *
 * 一个项目一个接入、一枚令牌，没有「令牌集合」这层路由：
 *
 *   GET    /me/mcp/projects                   我的项目列表（每项带它那枚令牌）
 *   POST   /me/mcp/projects                   新建（归属 = 自己，同一次响应给出首枚令牌明文）
 *   PUT    /me/mcp/projects/:id               修改（含启停）
 *   DELETE /me/mcp/projects/:id               删除（令牌一并删除）
 *   POST   /me/mcp/projects/:id/token         生成或轮换那枚令牌；明文只在这条响应里
 *   GET    /me/mcp/projects/:id/token/secret  取回明文（复制用）
 */
const P = "/me/mcp/projects";

export const selfMCPProjectEndpoints = {
  list: () => P,
  create: () => P,
  update: (id: string) => `${P}/${encodeURIComponent(id)}`,
  remove: (id: string) => `${P}/${encodeURIComponent(id)}`,
  issueToken: (id: string) => `${P}/${encodeURIComponent(id)}/token`,
  tokenSecret: (id: string) => `${P}/${encodeURIComponent(id)}/token/secret`,
};
