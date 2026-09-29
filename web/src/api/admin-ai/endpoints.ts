import type { ConfigTarget } from './type'

/**
 * 管理端接口路径与请求体形状集中在这一个文件，
 * 后端最终路由装配后如有出入，只需要改这里。
 *
 * 目前是按设计文档（平台协议配置化设计 5.7）+ 后端 service 层签名做的假设：
 *   GET  /admin/ai/{providers|models}                  列表
 *   GET  /admin/ai/{providers|models}/:key             详情（草稿 + 已发布）
 *   POST /admin/ai/{providers|models}                  新建草稿，body = saveBody(...)
 *   PUT  /admin/ai/{providers|models}/:key             更新草稿，body = saveBody(...)
 *   POST /admin/ai/{providers|models}/:key/validate    校验，body = {body}（不传 body 则校验最新草稿）
 *   POST /admin/ai/{providers|models}/:key/publish     发布
 *   GET  /admin/ai/{providers|models}/:key/revisions   历史版本
 *   POST /admin/ai/{providers|models}/:key/rollback    回滚，body = {revision_id}
 *   PUT  /admin/ai/models/:key/enabled                 上下架，body = {enabled}
 *   POST /admin/ai/models/:key/dry-run                 body = {input, use_provider_draft}
 *   POST /admin/ai/models/:key/test-run                body = {input, use_provider_draft} -> 任务视图
 *   GET  /admin/ai/test-runs/:id                       查询试跑任务
 *   POST /admin/ai/import/runninghub                   body = {webapp_id, kind, provider}
 *   GET  /admin/ai/secrets                             凭证状态列表（只有是否已设置）
 *   PUT  /admin/ai/secrets/:name                       body = {value}，只写
 */
const collection = (target: ConfigTarget) => (target === 'provider' ? 'providers' : 'models')

export const adminAiEndpoints = {
  list: (target: ConfigTarget) => `/admin/ai/${collection(target)}`,
  detail: (target: ConfigTarget, key: string) =>
    `/admin/ai/${collection(target)}/${encodeURIComponent(key)}`,
  create: (target: ConfigTarget) => `/admin/ai/${collection(target)}`,
  update: (target: ConfigTarget, key: string) =>
    `/admin/ai/${collection(target)}/${encodeURIComponent(key)}`,
  validate: (target: ConfigTarget, key: string) =>
    `/admin/ai/${collection(target)}/${encodeURIComponent(key)}/validate`,
  publish: (target: ConfigTarget, key: string) =>
    `/admin/ai/${collection(target)}/${encodeURIComponent(key)}/publish`,
  revisions: (target: ConfigTarget, key: string) =>
    `/admin/ai/${collection(target)}/${encodeURIComponent(key)}/revisions`,
  rollback: (target: ConfigTarget, key: string) =>
    `/admin/ai/${collection(target)}/${encodeURIComponent(key)}/rollback`,
  setEnabled: (key: string) => `/admin/ai/models/${encodeURIComponent(key)}/enabled`,
  dryRun: (key: string) => `/admin/ai/models/${encodeURIComponent(key)}/dry-run`,
  testRun: (key: string) => `/admin/ai/models/${encodeURIComponent(key)}/test-run`,
  testRunResult: (taskId: number | string) => `/admin/ai/test-runs/${taskId}`,
  importRunningHub: () => '/admin/ai/import/runninghub',
  secrets: () => '/admin/ai/secrets',
  secret: (name: string) => `/admin/ai/secrets/${encodeURIComponent(name)}`,
} as const

/** 保存草稿的请求体：正文放 body，备注放 note */
export const saveBody = (config: unknown, note?: string) => ({
  body: config,
  ...(note ? { note } : {}),
})
