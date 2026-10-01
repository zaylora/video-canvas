/**
 * AI 管理接口的类型，以 backend/docs/admin-ai-api.md 为准。
 * 三层对象：插件（JS 文件，版本不可变）→ 渠道（插件版本 + base_url + 加密 Key）→ 模型（渠道 + 上游模型 + capabilities）。
 */

/** 管理端角色：admin 运营（只管模型），super_admin 运维（能管插件与渠道） */
export type AdminRole = "admin" | "super_admin";

/** GET /admin/ai/me */
export interface AdminMe {
  /** 当前用户 ID */
  user_id: number;
  /** 当前角色；未知取值按 admin 处理 */
  role: AdminRole | string;
}

/** 校验 / 预检问题，path 是 JSON 路径（如 meta.endpoints.video.mode） */
export interface ConfigIssue {
  /** 出问题的 JSON 路径 */
  path: string;
  /** 问题描述 */
  message: string;
}

// ---------------------------------------------------------------- 插件

/** 插件来源：随仓库内置 / 在线上传 */
export type PluginSource = "builtin" | "uploaded";

/** 插件鉴权方式 */
export type PluginAuthType = "none" | "bearer" | "header" | "query" | "custom";

/** 渠道设置项 / 导入参数的类型 */
export type SettingType = "string" | "number" | "boolean" | "enum";

/** 插件声明的一个设置项（channelSettings / import.args 的值） */
export interface SettingSpec {
  /** 取值类型 */
  type: SettingType | string;
  /** 表单标题 */
  label: string;
  /** 补充说明 */
  description?: string;
  /** 是否必填 */
  required?: boolean;
  /** 默认值 */
  default?: unknown;
  /** enum 的可选值 */
  options?: string[];
}

/** 有序对象：键的书写顺序就是表单渲染顺序 */
export type SettingSchema = Record<string, SettingSpec>;

/** 插件导出的 meta（pluginmeta.Meta，字段名与 JS 里一致，驼峰） */
export interface PluginMeta {
  /** 契约版本 */
  apiVersion?: number;
  /** 插件 key */
  key?: string;
  /** 显示名 */
  name?: string;
  /** semver 版本号 */
  version?: string;
  /** 描述 */
  description?: string;
  /** 鉴权声明；name 用于 header（头名）与 query（参数名） */
  auth?: { type?: PluginAuthType | string; name?: string } | null;
  /** 结果下载可能访问的域名 */
  allowedHosts?: string[] | null;
  /** 支持的生成方式，键是模型 kind */
  endpoints?: Record<string, { mode?: "sync" | "async" | string }> | null;
  /** 渠道上的额外设置 */
  channelSettings?: SettingSchema | null;
  /** 「从渠道导入模型」的参数表单；没有该字段表示插件不支持导入 */
  import?: { args?: SettingSchema | null } | null;
  /** 异步轮询节奏（秒） */
  poll?: Record<string, number> | null;
}

/** 插件的一个不可变版本 */
export interface PluginVersionView {
  /** 版本 ID（渠道的 plugin_version_id） */
  id: number;
  /** 所属插件 */
  plugin_key: string;
  /** semver 版本号 */
  version: string;
  /** 代码的十六进制 sha256 */
  sha256: string;
  /** 登记时间 */
  created_at: string;
  /** 上传人（内置插件为 0） */
  created_by: number;
  /** 固定在这个版本上的渠道数 */
  channel_count: number;
  /** 预检时读出的 meta */
  meta: PluginMeta | null;
}

/** GET /admin/ai/plugins 的一项 */
export interface PluginView {
  /** 插件 key */
  key: string;
  /** 显示名 */
  name: string;
  /** 来源 */
  source: PluginSource | string;
  /** 是否启用；停用后所有使用它的渠道不再接新任务 */
  enabled: boolean;
  /** 更新时间 */
  updated_at: string;
  /** 版本列表，新到旧 */
  versions: PluginVersionView[];
}

/** POST /admin/ai/plugins（上传）的结果：预检不通过也是 200 */
export interface PluginUploadResult {
  /** 是否通过预检并登记 */
  accepted: boolean;
  /** 预检问题 */
  issues: ConfigIssue[];
  /** 通过时是新登记的版本 */
  version: PluginVersionView | null;
}

// ---------------------------------------------------------------- 渠道

/** 渠道限流：0 或缺省表示不限 */
export interface ChannelRateLimit {
  /** 每秒请求数 */
  rps?: number;
  /** 最大并发 */
  max_concurrency?: number;
}

/** 渠道视图（不含 Key） */
export interface ChannelView {
  /** 渠道 key */
  key: string;
  /** 显示名 */
  name: string;
  /** 使用的插件 */
  plugin_key: string;
  /** 固定的插件版本 ID */
  plugin_version_id: number;
  /** 固定的插件版本号 */
  plugin_version: string;
  /** 插件请求只能去这个地址 */
  base_url: string;
  /** 允许 base_url 解析到内网地址 */
  trusted_internal: boolean;
  /** 允许把 Key 交给 auth: custom 的插件 */
  allow_credentials: boolean;
  /** 插件 channelSettings 的取值 */
  settings: Record<string, unknown> | null;
  /** 限流 */
  rate_limit: ChannelRateLimit | null;
  /** 是否启用 */
  enabled: boolean;
  /** Key 是否已设置（只写不读） */
  secret_set: boolean;
  /** 最后修改人 */
  updated_by: number;
  /** 更新时间 */
  updated_at: string;
  /** 创建时间 */
  created_at: string;
}

/** POST /admin/ai/channels 的请求体 */
export interface ChannelCreateRequest {
  key: string;
  name: string;
  plugin_key: string;
  /** semver 字符串，固定到这个版本 */
  plugin_version: string;
  base_url: string;
  trusted_internal?: boolean;
  allow_credentials?: boolean;
  settings?: Record<string, unknown>;
  rate_limit?: ChannelRateLimit;
  enabled?: boolean;
}

/** PUT /admin/ai/channels/:key 的请求体：字段都可选，不传表示不改 */
export type ChannelUpdateRequest = Partial<Omit<ChannelCreateRequest, "key">>;

/** 连通性检查结果 */
export interface ChannelCheckResult {
  /** 是否连通 */
  ok: boolean;
  /** 说明，如 HTTP 200 */
  message: string;
  /** 耗时（毫秒） */
  duration_ms: number;
}

/** 从渠道导入得到的一份模型草稿（只预填编辑器，不落库） */
export interface ModelDraft {
  /** 上游模型名 */
  upstream_model: string;
  /** 模型 kind */
  kind: string;
  /** 展示名 */
  label: string;
  /** 固定参数 */
  params?: Record<string, unknown> | null;
  /** 插件给的生成参数预填建议（参数名 -> 建议），只在导入时预填编辑器 */
  param_hints?: import("@/utils/admin/param-hints").ParamHints | null;
}

/** POST /admin/ai/channels/:key/import 的结果 */
export interface ChannelImportResult {
  drafts: ModelDraft[];
}

// ---------------------------------------------------------------- 模型

/** 管理端模型列表的一行（不含正文） */
export interface ConfigListItem {
  /** 模型 key */
  key: string;
  /** 展示名称（旧字段，新后端用 label） */
  name?: string;
  /** 展示名称；后端补齐前可能缺失，缺失时界面回退到 key */
  label?: string;
  /** 模型类型 */
  kind?: string;
  /** 厂商 slug（对应内置 logo），没有为空串；后端补齐前可能缺失 */
  vendor?: string;
  /** 展示标签，没有为空数组；后端补齐前可能缺失 */
  tags?: string[];
  /** 模型使用的渠道 key（channels[0].channel）；后端补齐前可能缺失，缺失时不显示渠道 */
  channel?: string;
  /** 是否已上架 */
  enabled?: boolean;
  /** 排序值 */
  sort?: number;
  /** 已发布版本 ID，未发布为 null */
  published_revision_id: number | null;
  /** 已发布版本号，未发布为 null */
  published_revision_no: number | null;
  /** 草稿版本号，无草稿为 null */
  draft_revision_no: number | null;
  /** 是否存在尚未发布的草稿 */
  has_unpublished_draft: boolean;
  /** 最近更新时间 */
  updated_at: string;
}

/** 模型配置的一个历史版本 */
export interface ConfigRevision {
  /** 版本 ID */
  id: number;
  /** 配置目标类型（只剩 model） */
  target: string;
  /** 模型 key */
  target_key: string;
  /** 版本号 */
  revision_no: number;
  /** 配置正文（JSON） */
  body_json: unknown;
  /** 版本状态：草稿 / 已发布 / 已归档 */
  status: "draft" | "published" | "archived";
  /** 创建人用户 ID */
  created_by: number;
  /** 版本备注 */
  note: string;
  /** 创建时间 */
  created_at: string;
}

/** 模型详情：草稿与已发布正文 */
export interface ConfigDetail {
  /** 配置目标类型 */
  target: string;
  /** 模型 key */
  key: string;
  /** 模型类型 */
  kind?: string;
  /** 是否已上架 */
  enabled?: boolean;
  /** 排序值 */
  sort?: number;
  /** 最新草稿，无草稿为 null */
  draft: ConfigRevision | null;
  /** 当前已发布版本，未发布为 null */
  published: ConfigRevision | null;
  /** 最近更新时间 */
  updated_at: string;
}

/** 保存草稿的结果 */
export interface SaveDraftResult {
  /** 保存后的草稿版本 */
  revision: ConfigRevision;
  /** 校验发现的问题 */
  issues: ConfigIssue[];
}

/** 校验配置的结果 */
export interface ValidateResult {
  /** 是否通过校验 */
  valid: boolean;
  /** 校验发现的问题 */
  issues: ConfigIssue[];
}

// ---------------------------------------------------------------- 试跑追踪

/** 一次钩子调用 */
export interface TraceHook {
  /** 钩子名，如 buildSubmitRequest */
  name: string;
  /** 输入（ctx 等参数，JSON） */
  input?: unknown;
  /** 输出（返回值，JSON） */
  output?: unknown;
  /** utils.log 的输出 */
  logs?: string[] | null;
}

/** 发出的请求（已脱敏） */
export interface TraceRequest {
  method: string;
  url: string;
  headers?: Record<string, string> | null;
  body?: string;
}

/** 收到的响应（已脱敏、截断） */
export interface TraceResponse {
  status: number;
  body?: string;
  /** 响应体是否被截断 */
  truncated?: boolean;
}

/** 一个追踪步骤：一次钩子调用（kind=hook）或一次 HTTP 请求（kind=http） */
export interface TraceStep {
  /** 步骤名，如 submit / query / prepare:0 */
  name: string;
  /** hook | http */
  kind: "hook" | "http" | string;
  hook?: TraceHook | null;
  request?: TraceRequest | null;
  response?: TraceResponse | null;
  /** 出错信息 */
  error?: string;
  /** 耗时（毫秒） */
  duration_ms: number;
}

/** GET /admin/ai/test-runs/:id/trace */
export interface TestRunTrace {
  steps: TraceStep[];
}
