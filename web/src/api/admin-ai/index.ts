import service from '@/utils/requests/service'
import type { TaskView } from '@/api/generation-task/type'
import { adminAiEndpoints as ep, saveBody } from './endpoints'
import type {
  ConfigDetail,
  ConfigListItem,
  ConfigRevision,
  ConfigTarget,
  ImportResult,
  SaveDraftResult,
  SecretStatus,
  ValidateResult,
} from './type'

/**
 * 获取配置列表
 * @param target 配置类型（provider / model）
 * @returns 配置列表
 */
export const listConfigs = async (target: ConfigTarget) =>
  (await service.get<ConfigListItem[] | null>(ep.list(target), undefined)) ?? []

/**
 * 获取配置详情（草稿 + 已发布）
 * @param target 配置类型
 * @param key 配置 key
 * @returns 配置详情
 */
export const getConfigDetail = (target: ConfigTarget, key: string) =>
  service.get<ConfigDetail>(ep.detail(target, key), undefined)

/**
 * 新建配置草稿
 * @param target 配置类型
 * @param config 配置正文
 * @param note 备注
 * @returns 保存结果
 */
export const createDraft = (target: ConfigTarget, config: unknown, note?: string) =>
  service.post<SaveDraftResult>(ep.create(target), saveBody(config, note))

/**
 * 更新配置草稿
 * @param target 配置类型
 * @param key 配置 key
 * @param config 配置正文
 * @param note 备注
 * @returns 保存结果
 */
export const updateDraft = (target: ConfigTarget, key: string, config: unknown, note?: string) =>
  service.put<SaveDraftResult>(ep.update(target, key), saveBody(config, note))

/**
 * 校验配置；config 不传就校验已保存的最新草稿
 * @param target 配置类型
 * @param key 配置 key
 * @param config 待校验的配置正文
 * @returns 校验结果
 */
export const validateConfig = (target: ConfigTarget, key: string, config?: unknown) =>
  service.post<ValidateResult>(
    ep.validate(target, key),
    config === undefined ? {} : { body: config }
  )

/**
 * 发布配置草稿
 * @param target 配置类型
 * @param key 配置 key
 * @returns 新发布的版本
 */
export const publishConfig = (target: ConfigTarget, key: string) =>
  service.post<ConfigRevision>(ep.publish(target, key), undefined)

/**
 * 获取配置历史版本
 * @param target 配置类型
 * @param key 配置 key
 * @returns 历史版本列表
 */
export const listRevisions = async (target: ConfigTarget, key: string) =>
  (await service.get<ConfigRevision[] | null>(ep.revisions(target, key), undefined)) ?? []

/**
 * 回滚到指定历史版本
 * @param target 配置类型
 * @param key 配置 key
 * @param revisionId 目标版本 ID
 * @returns 回滚后的版本
 */
export const rollbackConfig = (target: ConfigTarget, key: string, revisionId: number) =>
  service.post<ConfigRevision>(ep.rollback(target, key), { revision_id: revisionId })

/**
 * 模型上下架
 * @param key 模型 key
 * @param enabled 是否启用
 */
export const setModelEnabled = (key: string, enabled: boolean) =>
  service.put<unknown>(ep.setEnabled(key), { enabled })

/**
 * 模型 dry-run（只构造请求，不真正调用）
 * @param key 模型 key
 * @param input 模型输入
 * @param useProviderDraft 是否使用 provider 草稿
 * @returns dry-run 结果
 */
export const dryRunModel = (key: string, input: unknown, useProviderDraft: boolean) =>
  service.post<unknown>(ep.dryRun(key), { input, use_provider_draft: useProviderDraft })

/**
 * 模型试跑，返回任务视图
 * @param key 模型 key
 * @param input 模型输入
 * @param useProviderDraft 是否使用 provider 草稿
 * @returns 试跑任务视图
 */
export const testRunModel = (key: string, input: unknown, useProviderDraft: boolean) =>
  service.post<TaskView>(ep.testRun(key), { input, use_provider_draft: useProviderDraft })

/**
 * 查询试跑任务
 * @param taskId 任务 ID
 * @returns 任务视图
 */
export const getTestRun = (taskId: number | string) =>
  service.get<TaskView>(ep.testRunResult(taskId), undefined)

/**
 * 导入 RunningHub 应用
 * @param webappId RunningHub webapp ID
 * @param kind 模型类型，默认 video
 * @param providerKey provider key，默认 runninghub
 * @returns 导入结果
 */
export const importRunningHub = (webappId: string, kind = 'video', providerKey = 'runninghub') =>
  service.post<ImportResult>(
    ep.importRunningHub(),
    { webapp_id: webappId, kind, provider: providerKey }
  )

/**
 * 获取凭证状态列表（只有是否已设置）
 * @returns 凭证状态列表
 */
export const listSecrets = async () =>
  (await service.get<SecretStatus[] | null>(ep.secrets(), undefined)) ?? []

/**
 * 设置凭证；只写：设置后任何接口都读不回明文
 * @param name 凭证名称
 * @param value 凭证明文
 */
export const setSecret = (name: string, value: string) =>
  service.put<unknown>(ep.secret(name), { value })
