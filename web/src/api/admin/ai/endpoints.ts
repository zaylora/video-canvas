/**
 * AI 管理接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 backend/docs/admin-ai-api.md；后端路由如有出入，只需要改这里。
 *
 *   GET    /admin/ai/me                                当前角色
 *   GET    /admin/ai/stats?days=7|30                   总览页任务统计
 *   GET    /admin/ai/overview                          总览页汇总（数量、待处理清单、精简渠道）
 *   GET    /admin/ai/plugins                           插件分页列表（?q=&kind=&source=&status=&page=&page_size=）
 *   GET    /admin/ai/plugins/options                   全部插件（带版本和 meta，不分页）
 *   GET    /admin/ai/plugins/:key                      插件详情
 *   POST   /admin/ai/plugins                           上传插件（multipart，字段 file）
 *   PUT    /admin/ai/plugins/:key/enabled              启停，body = {enabled}
 *   DELETE /admin/ai/plugins/:key/versions/:version    删除未被引用的版本
 *   POST   /admin/ai/plugins/:key/upgrade-models       批量把旧版本的模型升到指定 / 最新版本，body = {version?}
 *   GET    /admin/ai/{plugins|channels|models}/:key/delete-check   删除预检：谁在引用它
 *   DELETE /admin/ai/{plugins|channels|models}/:key    彻底删除（key 之后可以重新使用）
 *   GET    /admin/ai/channels                          渠道分页列表（?q=&status=&page=&page_size=）
 *   GET    /admin/ai/channels/options                  全部渠道（带健康状态，不分页，渠道顺序）
 *   GET    /admin/ai/channels/:key                     渠道详情
 *   GET    /admin/ai/channels/loads                    各渠道当前的生成中 / 排队任务数
 *   POST   /admin/ai/channels                          新建渠道
 *   PUT    /admin/ai/channels/:key                     更新渠道（字段都可选）
 *   PUT    /admin/ai/channels/:key/secret              设置 API Key，body = {value}，只写
 *   PUT    /admin/ai/channels/:key/secret-key          设置 / 清除 Secret Key（空串清除），body = {value}，只写
 *   PUT    /admin/ai/channels/sort                     渠道顺序，body = {keys: 全部渠道的新顺序}
 *   POST   /admin/ai/channels/:key/pull-models         向上游拉取模型清单（不落库）
 *   POST   /admin/ai/channels/:key/models/import       导入所选模型为“待配置”，body = {ids}
 *   PUT    /admin/ai/channels/:key/models/sort         渠道内模型顺序，body = {keys: 该渠道全部模型的新顺序}
 *   GET    /admin/ai/models                            模型分页列表（?q=&kind=&channel=&status=&page=&page_size=）
 *   GET    /admin/ai/models/options                    全部模型的列表项（不分页）
 *   GET    /admin/ai/models/limits                     按上游模型名识别上下文窗口 / 最大输出（?name=&channel=）
 *   POST   /admin/ai/models                            新建（保存，默认未启用）
 *   GET|PUT  /admin/ai/models/:key                     详情 / 更新（已启用的保存即生效）
 *   POST   /admin/ai/models/:key/{validate|dry-run|test-run}
 *   PUT    /admin/ai/models/:key/{enabled|sort}        启用 / 停用、排序
 *   GET    /admin/ai/test-runs/:id[/trace]             试跑任务视图 / 追踪
 *   GET    /admin/ai/schema/model                      模型配置 JSON Schema
 */
import type { DeleteTarget } from "./type";

const P = "/admin/ai";
const seg = (value: string | number) => encodeURIComponent(String(value));
const model = (key: string) => `${P}/models/${seg(key)}`;
const channel = (key: string) => `${P}/channels/${seg(key)}`;

export const adminAiEndpoints = {
  me: () => `${P}/me`,
  stats: () => `${P}/stats`,
  overview: () => `${P}/overview`,

  plugins: () => `${P}/plugins`,
  pluginOptions: () => `${P}/plugins/options`,
  plugin: (key: string) => `${P}/plugins/${seg(key)}`,
  pluginEnabled: (key: string) => `${P}/plugins/${seg(key)}/enabled`,
  pluginVersion: (key: string, version: string) =>
    `${P}/plugins/${seg(key)}/versions/${seg(version)}`,
  pluginUpgradeModels: (key: string) => `${P}/plugins/${seg(key)}/upgrade-models`,

  channels: () => `${P}/channels`,
  channelOptions: () => `${P}/channels/options`,
  channelLoads: () => `${P}/channels/loads`,
  channel,
  channelSecret: (key: string) => `${channel(key)}/secret`,
  channelSecretKey: (key: string) => `${channel(key)}/secret-key`,
  channelSort: () => `${P}/channels/sort`,
  channelPullModels: (key: string) => `${channel(key)}/pull-models`,
  channelImportModels: (key: string) => `${channel(key)}/models/import`,
  channelModelSort: (key: string) => `${channel(key)}/models/sort`,

  models: () => `${P}/models`,
  modelOptions: () => `${P}/models/options`,
  modelLimits: () => `${P}/models/limits`,
  model,
  modelValidate: (key: string) => `${model(key)}/validate`,
  modelEnabled: (key: string) => `${model(key)}/enabled`,
  modelSort: (key: string) => `${model(key)}/sort`,
  modelDryRun: (key: string) => `${model(key)}/dry-run`,
  modelTestRun: (key: string) => `${model(key)}/test-run`,
  testRunResult: (taskId: number | string) => `${P}/test-runs/${seg(taskId)}`,
  testRunTrace: (taskId: number | string) => `${P}/test-runs/${seg(taskId)}/trace`,
  modelSchema: () => `${P}/schema/model`,
  deleteCheck: (target: DeleteTarget, key: string) => `${P}/${target}s/${seg(key)}/delete-check`,
  remove: (target: DeleteTarget, key: string) => `${P}/${target}s/${seg(key)}`,
} as const;

/** 保存的请求体：正文放 body，备注放 note */
export const saveBody = (config: unknown, note?: string) => ({
  body: config,
  ...(note ? { note } : {}),
});
