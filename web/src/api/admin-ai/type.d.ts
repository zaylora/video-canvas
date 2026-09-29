/** 配置目标：平台协议 / 模型工作流（对应后端 target=provider|model） */
export type ConfigTarget = 'provider' | 'model'

/** 后端 dsl.Issue：校验问题，path 是 JSON 路径 */
export interface ConfigIssue {
  /** 出问题的 JSON 路径 */
  path: string
  /** 问题描述 */
  message: string
}

/** 管理端列表的一行（后端 ConfigListItem，不含正文） */
export interface ConfigListItem {
  /** 配置 key */
  key: string
  /** 展示名称 */
  name?: string
  /** 模型类型（image / video 等） */
  kind?: string
  /** 所属 provider 的 key（仅模型配置有） */
  provider_key?: string
  /** 是否已上架 */
  enabled?: boolean
  /** 排序值 */
  sort?: number
  /** 已发布版本 ID，未发布为 null */
  published_revision_id: number | null
  /** 已发布版本号，未发布为 null */
  published_revision_no: number | null
  /** 草稿版本号，无草稿为 null */
  draft_revision_no: number | null
  /** 是否存在尚未发布的草稿 */
  has_unpublished_draft: boolean
  /** 最近更新时间 */
  updated_at: string
}

/** 配置的一个历史版本 */
export interface ConfigRevision {
  /** 版本 ID */
  id: number
  /** 配置目标类型 */
  target: ConfigTarget
  /** 配置 key */
  target_key: string
  /** 版本号 */
  revision_no: number
  /** 配置正文（JSON） */
  body_json: unknown
  /** 版本状态：草稿 / 已发布 / 已归档 */
  status: 'draft' | 'published' | 'archived'
  /** 创建人用户 ID */
  created_by: number
  /** 版本备注 */
  note: string
  /** 创建时间 */
  created_at: string
}

/** 后端 ConfigDetail：草稿与已发布正文 */
export interface ConfigDetail {
  /** 配置目标类型 */
  target: ConfigTarget
  /** 配置 key */
  key: string
  /** 展示名称 */
  name?: string
  /** 模型类型 */
  kind?: string
  /** 所属 provider 的 key */
  provider_key?: string
  /** 是否已上架 */
  enabled?: boolean
  /** 排序值 */
  sort?: number
  /** 最新草稿，无草稿为 null */
  draft: ConfigRevision | null
  /** 当前已发布版本，未发布为 null */
  published: ConfigRevision | null
  /** 最近更新时间 */
  updated_at: string
}

/** 保存草稿的结果 */
export interface SaveDraftResult {
  /** 保存后的草稿版本 */
  revision: ConfigRevision
  /** 校验发现的问题 */
  issues: ConfigIssue[]
}

/** 校验配置的结果 */
export interface ValidateResult {
  /** 是否通过校验 */
  valid: boolean
  /** 校验发现的问题 */
  issues: ConfigIssue[]
}

/** RunningHub 应用导入时解析出的一个节点字段 */
export interface ImportNode {
  /** RunningHub 节点 ID */
  node_id: string
  /** 字段名 */
  field_name: string
  /** 字段默认值 */
  field_value: unknown
  /** 字段类型 */
  field_type?: string
  /** 字段描述 */
  description?: string
  /** 映射到模型输入的字段名 */
  input_name: string
  /** 映射到模型输入的字段类型 */
  input_type: string
}

/** 导入 RunningHub 应用的结果 */
export interface ImportResult {
  /** 生成的模型配置草稿 */
  draft: unknown
  /** 解析出的节点字段 */
  nodes: ImportNode[]
  /** 导入过程中的警告 */
  warnings: string[]
}

/** 凭证状态（只有是否已设置，不含明文） */
export interface SecretStatus {
  /** 凭证名称 */
  name: string
  /** 是否已设置 */
  is_set: boolean
  /** 最近更新时间，未设置为 null */
  updated_at: string | null
  /** 最近更新人用户 ID */
  updated_by: number
  /** 引用该凭证的配置 key 列表 */
  referenced_by: string[]
}
