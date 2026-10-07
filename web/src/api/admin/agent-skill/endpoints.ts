/**
 * Agent 技能管理接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 docs/design/Agent技能管理/Agent技能管理.md 第 8.2 节；后端路由如有出入，只需要改这里。
 * 全部需要 admin / super_admin。
 *
 *   GET    /admin/agent/skills                              列表（内置 + 导入），q / status 过滤
 *   POST   /admin/agent/skills/imports                      上传并预检（multipart）
 *   GET    /admin/agent/skills/imports/:id/files?path=      预览暂存包里的一个文件
 *   DELETE /admin/agent/skills/imports/:id                  放弃暂存
 *   POST   /admin/agent/skills/imports/:id/confirm          确认入库
 *   GET    /admin/agent/skills/:name                        详情（含版本列表）
 *   PUT    /admin/agent/skills/:name                        改显示名，body = {title}
 *   PUT    /admin/agent/skills/:name/enabled                启停，body = {enabled}
 *   PUT    /admin/agent/skills/:name/active-version         设为生效版本，body = {version}
 *   GET    /admin/agent/skills/:name/delete-check           删除预检
 *   DELETE /admin/agent/skills/:name                        删除技能
 *   GET    /admin/agent/skills/:name/versions/:v            某版本的清单、问题、正文
 *   GET    /admin/agent/skills/:name/versions/:v/files?path= 读版本里的一个文件
 *   GET    /admin/agent/skills/:name/versions/:v/download   下载整包（zip 流）
 *   DELETE /admin/agent/skills/:name/versions/:v            删除版本
 */
const P = "/admin/agent/skills";
const seg = (value: string | number) => encodeURIComponent(String(value));
const skill = (name: string) => `${P}/${seg(name)}`;
const version = (name: string, v: number) => `${skill(name)}/versions/${seg(v)}`;

export const adminAgentSkillEndpoints = {
  list: () => P,
  detail: skill,
  rename: skill,
  remove: skill,
  enabled: (name: string) => `${skill(name)}/enabled`,
  activeVersion: (name: string) => `${skill(name)}/active-version`,
  deleteCheck: (name: string) => `${skill(name)}/delete-check`,
  version,
  versionFile: (name: string, v: number) => `${version(name, v)}/files`,
  versionDownload: (name: string, v: number) => `${version(name, v)}/download`,
  imports: () => `${P}/imports`,
  importFile: (id: string) => `${P}/imports/${seg(id)}/files`,
  importDiscard: (id: string) => `${P}/imports/${seg(id)}`,
  importConfirm: (id: string) => `${P}/imports/${seg(id)}/confirm`,
};
