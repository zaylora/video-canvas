/**
 * Agent 分析令牌接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 docs/design/Agent运行观测/Agent运行观测.md 第 6.4、8.7 节；后端路由如有出入，只需要改这里。
 * 只接受登录 JWT，需要 admin / super_admin。
 *
 *   GET    /admin/agent/tokens        令牌列表（不含明文）
 *   POST   /admin/agent/tokens        签发，body = {name, days}；明文只在这次响应里返回
 *   DELETE /admin/agent/tokens/:id    注销
 */
const P = "/admin/agent/tokens";

export const adminAgentTokenEndpoints = {
  list: () => P,
  issue: () => P,
  revoke: (id: number | string) => `${P}/${encodeURIComponent(String(id))}`,
};
