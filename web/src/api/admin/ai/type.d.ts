/**
 * AI 管理接口的类型，以 backend/docs/admin-ai-api.md 为准。
 * 三层对象：插件（JS 文件，版本不可变）→ 渠道（只有连接信息：base_url、Key、请求头、限流）→ 模型（渠道 + 上游模型 + 协议 + capabilities）。
 * 协议插件、版本与协议设置项在模型的 channels[0].protocol 里选，渠道不绑协议。
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

/** 协议设置项（插件的 channelSettings）的类型 */
export type SettingType = "string" | "number" | "boolean" | "enum";

/** 插件声明的一个协议设置项（channelSettings 的值） */
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
  /** 厂商 slug（可选），模型弹窗的协议列表里显示 */
  vendor?: string;
  /** 支持的生成方式，键是模型 kind；method 与 path 只用于协议列表的展示（“POST /v1/messages”） */
  endpoints?: Record<
    string,
    { mode?: "sync" | "async" | string; method?: string; path?: string }
  > | null;
  /** 协议设置项：在模型的“协议设置”里填，值存在模型配置里 */
  channelSettings?: SettingSchema | null;
  /** 已弃用：渠道不再绑协议，拉取模型由宿主内置的通用请求完成，宿主不再调用导入钩子 */
  import?: { args?: SettingSchema | null } | null;
  /** 异步轮询节奏（秒） */
  poll?: Record<string, number> | null;
}

/** 插件的一个不可变版本 */
export interface PluginVersionView {
  /** 版本 ID */
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
  /** 协议固定在这个版本上的模型数 */
  model_count: number;
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
  /** 是否启用；停用后所有协议用了它的模型不再接新任务 */
  enabled: boolean;
  /** 更新时间 */
  updated_at: string;
  /** 版本列表，新到旧 */
  versions: PluginVersionView[];
}

/** 上传时版本号被自动调整：代码里写的版本号缺失、不合法、已被占用或不比已有版本高 */
export interface PluginVersionAdjusted {
  /** 代码里写的版本号；空串表示没写或写得不合法 */
  from: string;
  /** 实际登记的版本号 */
  to: string;
}

/** POST /admin/ai/plugins（上传）的结果：预检不通过也是 200 */
export interface PluginUploadResult {
  /** 是否通过预检并登记 */
  accepted: boolean;
  /** 预检问题 */
  issues: ConfigIssue[];
  /** 通过时是新登记的版本；未通过时为 null（拿不到插件 key） */
  version: PluginVersionView | null;
  /** 版本号被自动调整时才有；没有调整、预检未通过时缺省或为 null */
  version_adjusted?: PluginVersionAdjusted | null;
}

// ---------------------------------------------------------------- 渠道

/** 渠道限流：0 或缺省表示不限 */
export interface ChannelRateLimit {
  /** 每秒请求数 */
  rps?: number;
  /** 最大同时请求数：同时发往上游的 HTTP 请求数（不是任务数） */
  max_concurrency?: number;
  /** 最大同时生成数：同时在上游生成的任务数，超出的任务在平台排队 */
  max_running?: number;
}

/** 渠道当前的任务负载（没有未完成任务的渠道不会出现） */
export interface ChannelLoad {
  /** 渠道 key */
  channel: string;
  /** 同时生成数：已在上游生成 + 正在提交的任务，和 max_running 比较 */
  running: number;
  /** 排队数：还没调用上游、在等名额的任务 */
  waiting: number;
}

/** 渠道视图（不含 Key）：渠道只管连接信息，协议在模型里选 */
export interface ChannelView {
  /** 渠道 key */
  key: string;
  /** 显示名 */
  name: string;
  /** 请求只能去这个地址 */
  base_url: string;
  /** 自定义请求头（明文存储，不要放密钥），只加到发往 base_url 同源的请求 */
  headers: Record<string, string>;
  /** 限流 */
  rate_limit: ChannelRateLimit | null;
  /** 渠道顺序，越小越靠前；用户看到的模型顺序 = 渠道顺序 + 渠道内顺序 */
  sort: number;
  /** 是否启用 */
  enabled: boolean;
  /** API Key 是否已设置（只写不读） */
  secret_set: boolean;
  /** Secret Key（可选的第二凭证，AK/SK 签名类协议用）是否已设置（只写不读） */
  secret_key_set: boolean;
  /** 最后修改人 */
  updated_by: number;
  /** 更新时间 */
  updated_at: string;
  /** 创建时间 */
  created_at: string;
}

/** POST /admin/ai/channels 的请求体（Key 与 Secret Key 另走 secret 接口） */
export interface ChannelCreateRequest {
  /** 不传由后端生成（16 位十六进制）；渠道 key 是内部标识，创建后不可改 */
  key?: string;
  name: string;
  base_url: string;
  headers?: Record<string, string>;
  rate_limit?: ChannelRateLimit;
  enabled?: boolean;
}

/** PUT /admin/ai/channels/:key 的请求体：字段都可选，不传表示不改；headers 传了就整体替换 */
export type ChannelUpdateRequest = Partial<Omit<ChannelCreateRequest, "key">>;

/** 可以删除的对象 */
export type DeleteTarget = "plugin" | "channel" | "model";

/**
 * 删除阻断原因：
 * - model_enabled：模型还在上架
 * - channel_models：有模型（最新草稿或已发布版本）在用这个渠道，refs 是这些模型
 * - plugin_models：有模型的协议固定在这个插件的某个版本上，refs 是这些模型
 * - builtin_plugin：内置插件只能停用
 * - active_tasks：还有进行中的任务在用
 */
export type DeleteBlockerKind =
  | "model_enabled"
  | "channel_models"
  | "plugin_models"
  | "builtin_plugin"
  | "active_tasks";

export interface DeleteBlocker {
  kind: DeleteBlockerKind | (string & {});
  /** 给运营看的整句，带数量 */
  message: string;
  /** 引用它的对象 */
  refs: Array<{ key: string; name: string }>;
}

/** 删除预检结果：blockers 为空表示可以删 */
export interface DeleteCheckResult {
  blockers: DeleteBlocker[];
}

/** 拉取到的一个上游模型 */
export interface PulledModel {
  /** 上游模型 id */
  id: string;
  /** 这个渠道下已有同名上游模型（导入时会跳过） */
  exists: boolean;
}

/** POST /admin/ai/channels/:key/pull-models 的结果：不落库 */
export interface PullModelsResult {
  models: PulledModel[];
  total: number;
}

/** POST /admin/ai/channels/:key/models/import 的结果 */
export interface ImportModelsResult {
  /** 新建的“待配置”模型 */
  created: Array<{ key: string; upstream_model: string }>;
  /** 因渠道下已有同名上游模型而跳过的上游模型名 */
  skipped: string[];
}

/** POST /admin/ai/plugins/:key/upgrade-models 的结果：逐个处理，失败不中断 */
export interface UpgradeModelsResult {
  /** 实际升到的版本 */
  to_version: string;
  /** 升级成功的模型 key */
  upgraded: string[];
  /** 失败的模型与原因（保持原样） */
  failed: Array<{ key: string; reason: string }>;
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
  /** 上游模型名（channels[0].upstream_model） */
  upstream?: string;
  /** 协议选的插件 key（channels[0].protocol.plugin）；没选（待配置 / Agent）为空串 */
  plugin?: string;
  /** 协议固定的插件版本号；没选为空串 */
  plugin_version?: string;
  /** 是否已上架 */
  enabled?: boolean;
  /** 排序值 */
  sort?: number;
  /** 最近更新时间 */
  updated_at: string;
}

/** 模型详情：配置正文与启用状态 */
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
  /** 配置正文（JSON 对象） */
  body: unknown;
  /** 最近更新时间 */
  updated_at: string;
}

/** 保存的结果 */
export interface SaveModelResult {
  /** 保存的模型 key；新建时没填的由后端生成，从这里取 */
  key: string;
  /** 校验发现的问题（有问题也已保存，但启用会被拦） */
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

// ---------------------------------------------------------------- 总览统计

/** 统计范围：只有这两档 */
export type AdminStatsDays = 7 | 30;

/**
 * 一天的任务数（试跑不算）。三个桶之和是当天创建的全部正式任务，页面上所有数字用同一口径。
 */
export interface AdminStatsDay {
  /** 日期 YYYY-MM-DD，按上海时区的自然日 */
  date: string;
  /** 成功 */
  succeeded: number;
  /** 失败（含过期） */
  failed: number;
  /** 其他：已取消与仍在进行中 */
  other: number;
}

/** 一个模型的调用数 */
export interface AdminStatsModelCount {
  /** 模型 key */
  model: string;
  /** 展示名；模型已删除或没有展示名时等于 key */
  label: string;
  /** 区间内的任务数 */
  count: number;
}

/** 一种任务类型的调用数 */
export interface AdminStatsKindCount {
  /** video / image / audio / text */
  kind: string;
  /** 区间内的任务数 */
  count: number;
}

/** GET /admin/ai/stats：总览页的任务量柱形图与调用占比 */
export interface AdminStats {
  /** 统计天数 */
  days: AdminStatsDays;
  /** 恒为 days 项，日期升序，没有任务的日子是 0，最后一项是今天 */
  daily: AdminStatsDay[];
  /** 按任务数降序的全量模型 */
  by_model: AdminStatsModelCount[];
  /** 按任务数降序的任务类型 */
  by_kind: AdminStatsKindCount[];
}

// ---------------------------------------------------------------- 分页列表（渠道 / 插件 / 模型）

/** 渠道健康色调：ok 可用、warn 缺 Key、off 已停用 */
export type HealthTone = "ok" | "warn" | "bad" | "off";

/** 渠道实际能不能用（后端算好）：停用、没设 Key */
export interface ChannelHealth {
  tone: HealthTone;
  /** 列表里的短标签 */
  label: string;
  /** 不可用的原因（整句）；可用时为空串 */
  reason: string;
}

/** 模型对运营露出的唯一状态：在线 / 不可用（已上架但渠道或协议用不了）/ 未上线 / 待配置（还没选能力或协议） */
export type ModelStatus = "online" | "broken" | "offline" | "pending";

/** 模型状态（后端算好）；reason 是渠道或协议层面的问题，未上线的模型也可能有 */
export interface ModelHealth {
  status: ModelStatus;
  reason: string;
}

/** 通用分页结果 */
export interface AdminPage<T> {
  list: T[];
  total: number;
  page: number;
  page_size: number;
}

/** 渠道列表的一行：渠道 + 健康状态、模型数 */
export interface ChannelListItem extends ChannelView {
  health: ChannelHealth;
  model_count: number;
  /** 其中已上架的 */
  online_model_count: number;
}

/** 渠道 options（主从页左栏、弹窗下拉、概览用）：完整渠道视图 + 健康状态，不分页，顺序是渠道顺序 */
export interface ChannelOption extends ChannelView {
  health: ChannelHealth;
}

/** 插件列表的一行：插件（带全部版本）+ 模型数、旧版模型数、在线模型数、最新版本 */
export interface PluginListItem extends PluginView {
  /** 按 semver 最高的版本号；没有版本为空串 */
  latest_version: string;
  /** 协议用了这个插件的模型数 */
  model_count: number;
  /** 其中固定在旧版本上的模型数 */
  outdated_model_count: number;
  /** 其中已上架的模型数 */
  online_model_count: number;
}

/** 模型列表的一行：列表项 + 状态、渠道名和协议名 */
export interface ModelListItem extends ConfigListItem {
  health: ModelHealth;
  /** 绑定渠道的显示名；没绑或渠道不存在为空串 */
  channel_name: string;
  /** 协议插件的显示名；没选或插件不存在为空串 */
  plugin_name: string;
}

/** 渠道列表的查询参数 */
export interface ListChannelsParams {
  q?: string;
  /** ok / warn / off */
  status?: string;
  page?: number;
  page_size?: number;
}

/** 插件列表的查询参数 */
export interface ListPluginsParams {
  q?: string;
  kind?: string;
  /** builtin / uploaded */
  source?: string;
  /** on / off / outdated */
  status?: string;
  page?: number;
  page_size?: number;
}

/** 模型列表的查询参数 */
export interface ListModelsParams {
  q?: string;
  kind?: string;
  /** 渠道 key */
  channel?: string;
  /** online / broken / offline / pending */
  status?: string;
  page?: number;
  page_size?: number;
}

// ---------------------------------------------------------------- 总览汇总

/** 待处理项的修复动作 */
export interface OverviewTodoAction {
  /** set-key 设置 Key / open-plugin 去插件页 / open-channel 去渠道页 / upgrade-plugin 一键升级 */
  kind: "set-key" | "open-plugin" | "open-channel" | "upgrade-plugin";
  channel_key?: string;
  /** set-key 弹窗里要显示的渠道名 */
  channel_name?: string;
  plugin_key?: string;
}

/** 总览页“待处理”的一条（后端按严重度排好） */
export interface OverviewTodo {
  /** 稳定 id（React key） */
  id: string;
  tone: "bad" | "warn" | "info";
  text: string;
  action: OverviewTodoAction;
}

/** “渠道负载”卡片要的精简渠道 */
export interface OverviewChannel {
  key: string;
  name: string;
  /** 同时生成上限；0 表示不限 */
  max_running: number;
  health: ChannelHealth;
}

/** GET /admin/ai/overview：总览页的汇总，前端不再拉全量清单 */
export interface AdminOverview {
  plugins: { total: number; usable: number };
  channels: { total: number; usable: number; no_key: number };
  models: { total: number; online: number; broken: number; pending: number };
  todos: OverviewTodo[];
  channel_briefs: OverviewChannel[];
}
