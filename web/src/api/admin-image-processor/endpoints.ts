/**
 * 图片处理服务管理接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 docs/design/画布素材加载设计/图片处理服务接口契约.md 第 2.2 节；后端路由如有出入，只需要改这里。
 *
 *   GET    /admin/image-processors/presets        厂商预设
 *   GET    /admin/image-processors                处理服务列表
 *   GET    /admin/image-processors/:id            处理服务详情
 *   POST   /admin/image-processors                新建草稿（super_admin）
 *   PUT    /admin/image-processors/:id            保存草稿，带 version（super_admin）
 *   POST   /admin/image-processors/:id/check      校验 + 试跑（super_admin）
 *   POST   /admin/image-processors/:id/publish    发布草稿，body = {version}（super_admin）
 *   POST   /admin/image-processors/:id/rollback   回滚到上一个已发布版本（super_admin）
 *   POST   /admin/image-processors/:id/disable    停用（super_admin）
 *   DELETE /admin/image-processors/:id            删除 draft / disabled 的（super_admin）
 */
const P = "/admin/image-processors";
const seg = (value: string | number) => encodeURIComponent(String(value));
const one = (id: number) => `${P}/${seg(id)}`;

export const adminImageProcessorEndpoints = {
  list: () => P,
  presets: () => `${P}/presets`,
  detail: one,
  create: () => P,
  update: one,
  check: (id: number) => `${one(id)}/check`,
  publish: (id: number) => `${one(id)}/publish`,
  rollback: (id: number) => `${one(id)}/rollback`,
  disable: (id: number) => `${one(id)}/disable`,
  remove: one,
};
