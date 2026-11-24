/**
 * MCP 接入的后台接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供）。
 * 只接受登录 JWT，需要 admin / super_admin；删除项目仅 super_admin。
 *
 * 一个项目就是一个接入、只有一枚令牌，所以没有「令牌集合」这层路由：
 *
 *   GET    /admin/mcp/projects                   项目列表（每项带它那枚令牌）
 *   POST   /admin/mcp/projects                   新建项目（同一次响应里给出首枚令牌明文）
 *   PUT    /admin/mcp/projects/:id               修改项目（含启停）
 *   DELETE /admin/mcp/projects/:id               删除项目（令牌一并删除）
 *   POST   /admin/mcp/projects/:id/token         生成或轮换那枚令牌；明文只在这条响应里
 *   GET    /admin/mcp/projects/:id/token/secret  取回明文（复制用）
 */
const P = "/admin/mcp/projects";

export const adminMCPProjectEndpoints = {
  list: () => P,
  create: () => P,
  update: (id: string) => `${P}/${encodeURIComponent(id)}`,
  remove: (id: string) => `${P}/${encodeURIComponent(id)}`,
  issueToken: (id: string) => `${P}/${encodeURIComponent(id)}/token`,
  tokenSecret: (id: string) => `${P}/${encodeURIComponent(id)}/token/secret`,
};
