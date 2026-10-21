/**
 * Agent 运行记录接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 docs/design/Agent运行观测/Agent运行观测.md 第 6.7、6.8、8.7 节；后端路由如有出入，只需要改这里。
 * 全部只读，管理员登录 JWT 即可访问。rid 是后端编码后的运行 ID（32 位十六进制串）。
 *
 *   GET /admin/agent/runs               运行摘要列表，游标分页，创建时间倒序
 *   GET /admin/agent/runs/:rid          一次运行的完整详情（时间线、工具调用、模型调用……）
 *   GET /admin/agent/runs/:rid/export   导出，?format=md 返回 {format, content}
 *   GET /admin/agent/stats              版本对比：按 harness_key / model_key 分组的聚合指标
 */
const P = "/admin/agent";

export const adminAgentRunEndpoints = {
  list: () => `${P}/runs`,
  detail: (rid: string) => `${P}/runs/${encodeURIComponent(rid)}`,
  export: (rid: string) => `${P}/runs/${encodeURIComponent(rid)}/export`,
  stats: () => `${P}/stats`,
};
