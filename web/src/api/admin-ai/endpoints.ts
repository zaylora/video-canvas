/**
 * AI 管理接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 backend/docs/admin-ai-api.md；后端路由如有出入，只需要改这里。
 *
 *   GET    /admin/ai/me                                当前角色
 *   GET    /admin/ai/plugins                           插件与版本列表
 *   POST   /admin/ai/plugins                           上传插件（multipart，字段 file）
 *   PUT    /admin/ai/plugins/:key/enabled              启停，body = {enabled}
 *   DELETE /admin/ai/plugins/:key/versions/:version    删除未被引用的版本
 *   GET    /admin/ai/channels[/:key]                   渠道列表 / 详情
 *   POST   /admin/ai/channels                          新建渠道
 *   PUT    /admin/ai/channels/:key                     更新渠道（字段都可选）
 *   PUT    /admin/ai/channels/:key/secret              设置 Key，body = {value}，只写
 *   POST   /admin/ai/channels/:key/check               连通性检查
 *   POST   /admin/ai/channels/:key/import              导入模型草稿，body = {args}
 *   GET|POST /admin/ai/models                          模型列表 / 新建草稿
 *   GET|PUT  /admin/ai/models/:key                     详情 / 更新草稿
 *   POST   /admin/ai/models/:key/{validate|publish|rollback|dry-run|test-run}
 *   GET    /admin/ai/models/:key/revisions[/:rid]      历史
 *   PUT    /admin/ai/models/:key/{enabled|sort}        上下架 / 排序
 *   GET    /admin/ai/test-runs/:id[/trace]             试跑任务视图 / 追踪
 *   GET    /admin/ai/schema/model                      模型配置 JSON Schema
 */
const P = "/admin/ai";
const seg = (value: string | number) => encodeURIComponent(String(value));
const model = (key: string) => `${P}/models/${seg(key)}`;
const channel = (key: string) => `${P}/channels/${seg(key)}`;

export const adminAiEndpoints = {
  me: () => `${P}/me`,

  plugins: () => `${P}/plugins`,
  pluginEnabled: (key: string) => `${P}/plugins/${seg(key)}/enabled`,
  pluginVersion: (key: string, version: string) =>
    `${P}/plugins/${seg(key)}/versions/${seg(version)}`,

  channels: () => `${P}/channels`,
  channel,
  channelSecret: (key: string) => `${channel(key)}/secret`,
  channelCheck: (key: string) => `${channel(key)}/check`,
  channelImport: (key: string) => `${channel(key)}/import`,

  models: () => `${P}/models`,
  model,
  modelValidate: (key: string) => `${model(key)}/validate`,
  modelPublish: (key: string) => `${model(key)}/publish`,
  modelRollback: (key: string) => `${model(key)}/rollback`,
  modelRevisions: (key: string) => `${model(key)}/revisions`,
  modelRevision: (key: string, revisionId: number | string) =>
    `${model(key)}/revisions/${seg(revisionId)}`,
  modelEnabled: (key: string) => `${model(key)}/enabled`,
  modelSort: (key: string) => `${model(key)}/sort`,
  modelDryRun: (key: string) => `${model(key)}/dry-run`,
  modelTestRun: (key: string) => `${model(key)}/test-run`,
  testRunResult: (taskId: number | string) => `${P}/test-runs/${seg(taskId)}`,
  testRunTrace: (taskId: number | string) => `${P}/test-runs/${seg(taskId)}/trace`,
  modelSchema: () => `${P}/schema/model`,
} as const;

/** 保存草稿的请求体：正文放 body，备注放 note */
export const saveBody = (config: unknown, note?: string) => ({
  body: config,
  ...(note ? { note } : {}),
});
