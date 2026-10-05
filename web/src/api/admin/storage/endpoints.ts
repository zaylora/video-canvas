/**
 * 存储配置管理接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 docs/design/存储配置设计/存储配置设计.md 第 8.3 节；后端路由如有出入，只需要改这里。
 *
 *   GET    /admin/storages                      存储列表（含素材数、默认标记、最近测试结果）
 *   GET    /admin/storages/presets              服务商预设与地域列表
 *   GET    /admin/storages/:id                  存储详情
 *   POST   /admin/storages/test                 测试一份未保存的配置（super_admin，不落库）
 *   POST   /admin/storages                      新建（super_admin，保存前后端自动测试）
 *   PUT    /admin/storages/:id                  更新（super_admin，带 version）
 *   PUT    /admin/storages/:id/secret           替换 AccessKey ID 与 Secret（super_admin，先测试再替换）
 *   POST   /admin/storages/:id/check            用已存密钥重新测试（super_admin）
 *   PUT    /admin/storages/default              设为默认，body = {id}（super_admin）
 *   GET    /admin/storages/:id/delete-check     删除预检（super_admin）
 *   DELETE /admin/storages/:id                  删除（super_admin）
 */
const P = "/admin/storages";
const seg = (value: string | number) => encodeURIComponent(String(value));
const one = (id: number) => `${P}/${seg(id)}`;

export const adminStorageEndpoints = {
  list: () => P,
  presets: () => `${P}/presets`,
  detail: one,
  test: () => `${P}/test`,
  create: () => P,
  update: one,
  secret: (id: number) => `${one(id)}/secret`,
  check: (id: number) => `${one(id)}/check`,
  setDefault: () => `${P}/default`,
  deleteCheck: (id: number) => `${one(id)}/delete-check`,
  remove: one,
};
