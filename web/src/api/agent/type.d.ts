import type { GraphChange } from "@/utils/canvas/graph-changes";

/**
 * 任务模式：全能创作 / 剧本创编 / 分镜搭建 / 提示词优化 / 短剧助手（只有短剧画布能建，契约 §15）/
 * 后台助手（admin，不绑画布，只有管理员能建，契约见 docs/design/后台Agent助手/一期接口契约.md）
 */
export type AgentMode = "all" | "script" | "storyboard" | "prompt" | "drama" | "admin";

/** 运行状态 */
export type AgentRunStatus =
  /** 已创建，等待 runtime 接手 */
  | "queued"
  /** 运行中 */
  | "running"
  /** 等用户批准生成或删除 */
  | "waiting_approval"
  /** 等用户回答提问 */
  | "waiting_input"
  /** 服务重启后自动续跑中（只作为事件出现，不是落库的状态）：接上之前显示「正在恢复…」，接上后回到 running */
  | "recovering"
  /** 完成 */
  | "succeeded"
  /** 失败 */
  | "failed"
  /** 用户停止 */
  | "canceled"
  /** 早先设过额度、用完了，暂停，可继续（现在新的运行不设额度，不会再出现） */
  | "budget_exhausted"
  /** 达到步数上限，暂停，可继续 */
  | "step_limit"
  /** 超时 */
  | "timeout"
  /** 服务重启或 runtime 崩溃，可继续 */
  | "interrupted"
  /** 等待用户响应超过 24 小时 */
  | "expired";

/** Agent 可选的大语言模型 */
export interface AgentModelDto {
  /** 模型 key */
  key: string;
  /** 显示名 */
  name: string;
  /** 厂商 slug（如 claude），对应内置 logo；空串表示没有 */
  vendor?: string;
  /** 是否支持看图 */
  vision: boolean;
  /** 是否支持理解视频画面和声音；旧服务可能不返回 */
  video?: boolean;
  /** 上下文窗口（Token），没配置是 0 */
  context_window: number;
}

/** 会话 */
export interface AgentSessionDto {
  /** 会话 ID（十六进制串） */
  id: string;
  /** 所属画布 ID */
  canvas_id: string;
  /** 标题 */
  title: string;
  /** 任务模式 */
  mode: AgentMode;
  /** Agent 模型 key */
  model_key: string;
  /** 最近一条事件的序号，用来判断有没有漏事件 */
  last_seq: number;
  /** 创建时间 */
  created_at: string;
  /** 更新时间 */
  updated_at: string;
}

/** 一轮运行 */
export interface AgentRunDto {
  /** 运行 ID */
  id: string;
  /** 所属会话 */
  session_id: string;
  /** 所属画布 */
  canvas_id: string;
  /** 运行状态 */
  status: AgentRunStatus;
  /** 任务模式 */
  mode: AgentMode;
  /** 已花积分 */
  spent_credits: number;
  /** 当前上下文大小（Token）：最近一次对话调用的输入，还没有调用过是 0 */
  context_tokens: number;
  /** 已执行的工具调用步数 */
  steps: number;
  /** 步数上限 */
  max_steps: number;
  /** 失败时的错误码，没有为空串 */
  error_code: string;
  /** 失败时给用户看的文案，没有为空串 */
  error_message: string;
  /** 创建时间 */
  created_at: string;
  /** 结束时间，未结束为 null */
  ended_at: string | null;
}

/**
 * 审批种类：生成 / 删除 / 提问；短剧助手的写工具（改大纲、改剧本、改片段、提交任务）走 edit，
 * 前端对任何不认识的种类都按 edit 的通用修改卡展示（读 payload 的 title / reason / before / after）
 */
export type AgentApprovalKind =
  /** 生成节点 */
  | "generate"
  /** 删除节点 */
  | "delete"
  /** 提问 */
  | "ask"
  /** 修改短剧 */
  | "edit"
  /** 后台配置变更 */
  | "admin_change"
  /** 安全填写渠道密钥 */
  | "admin_secret"
  /** 真实试跑授权 */
  | "admin_test_run";

/** 审批状态 */
export type AgentApprovalStatus =
  | "pending"
  | "approved"
  | "partially_approved"
  | "rejected"
  | "expired"
  | "executed"
  | "failed";

/** 生成审批里的一项 */
export interface AgentGenerateItem {
  /** 要生成的节点 */
  node_id: string;
  /** 节点标题 */
  label: string;
  /** 生成模型 key */
  model_key: string;
  /** 生成模型显示名 */
  model_name: string;
  /** 单个预估积分 */
  price: number;
  /** 申请生成的个数 */
  count: number;
}

/** 生成审批的内容（approval.kind = generate） */
export interface AgentGeneratePayload {
  /** Agent 给的理由 */
  reason: string;
  /** 要生成的条目 */
  items: AgentGenerateItem[];
}

/** 删除审批的内容（approval.kind = delete） */
export interface AgentDeletePayload {
  /** Agent 给的理由 */
  reason: string;
  /** 要删的节点 id */
  node_ids: string[];
  /** 对应的节点标题 */
  labels: string[];
  /** 对应的节点是否含产物（卡片上标红提醒） */
  outputs: boolean[];
  /** 要删的连线 id */
  edge_ids: string[];
}

/** 提问里选择题的一个选项 */
export interface AgentAskOption {
  /** 选项文字 */
  label: string;
  /** 非空：选中后原地展开一个输入框，值是占位文字 */
  follow?: string;
}

/** 提问里的一道题（一张卡可以列 1 到 5 题） */
export interface AgentAskQuestion {
  /** 题目标识，同一张卡里唯一，回答按它对应 */
  id: string;
  /** 问题 */
  question: string;
  /** choice 选项题；model 选模型题；text / url / number 直接填写 */
  kind: "choice" | "model" | "text" | "url" | "number";
  /** 是否必答 */
  required: boolean;
  /** 选项（choice） */
  options?: AgentAskOption[];
  /** 允许多选（choice） */
  multi?: boolean;
  /** 多一个「其他，我来说」输入项（choice） */
  allow_custom?: boolean;
  /** 要选哪类模型（model） */
  model_kind?: "image" | "video";
  /** 可选的已发布模型（model） */
  models?: Array<{ key: string; name: string; hint: string }>;
  /** 输入框占位文字（text / url / number） */
  placeholder?: string;
}

/**
 * 提问的内容（approval.kind = ask）。
 * 新提问带 questions；顶层的 question / kind / options / models 是给旧数据和老客户端留的：
 * 单题时就是这一题，多题时 question 是卡片标题、其余是第一题。历史里的旧提问没有 questions。
 */
export interface AgentAskPayload {
  /** 问题；多题时是卡片标题（如「还差 3 件事要你定」） */
  question: string;
  /** choice 选项题；model 选模型题（旧数据只有这两种） */
  kind: "choice" | "model" | "text" | "url" | "number";
  /** 选项（choice） */
  options?: string[];
  /** 要选哪类模型（model） */
  model_kind?: "image" | "video";
  /** 是否允许自定义回答 */
  allow_custom?: boolean;
  /** 可选的已发布模型（model） */
  models?: Array<{ key: string; name: string; hint: string }>;
  /** 各题；旧提问没有 */
  questions?: AgentAskQuestion[];
}

/** 提问决定里逐题的回答（approval.decision.answers 的一项） */
export interface AgentAskDecidedAnswer {
  id: string;
  /** 题目原文 */
  question: string;
  /** 人类可读的一句；没回答是空串 */
  answer: string;
  /** 原始回答：字符串，多选是字符串数组；没回答是空串 */
  value: string | string[];
}

/** 通用修改审批的内容（approval.kind = edit，短剧助手）：before / after 是修改前后的文本摘要 */
export interface AgentEditPayload {
  /** 一句话标题，如「修改第 3 集剧本」 */
  title?: string;
  /** Agent 给的理由 */
  reason?: string;
  /** 修改前的摘要文本 */
  before?: string;
  /** 修改后的摘要文本 */
  after?: string;
}

/** 变更单里一条字段级差异 */
export interface AdminChangeDiff {
  /** 字段路径，如 "pricing.per_second"、"pricing.tiers[1].unit"、"default_markup_pct" */
  path: string;
  /** 改前的值（JSON），null 表示原来没有 */
  before: unknown;
  /** 改后的值（JSON），null 表示删除 */
  after: unknown;
}

/** 价格的计算依据：mode = cost 是上游成本加价，relative 是相对调价 */
export interface AdminPricingBasis {
  /** 对应 diff 里售价那一条的 path */
  path: string;
  /** 计算方式 */
  mode: "cost" | "relative";
  /** 上游报价（mode = cost） */
  upstream?: {
    amount: number;
    currency: "USD" | "CNY";
    unit: string;
    source_url: string;
    note?: string;
  };
  /** 汇率（mode = cost，上游是美元时） */
  fx?: { rate: number; date: string | null; source: "live" | "override"; stale: boolean };
  /** 1 元折合的积分（mode = cost） */
  credits_per_yuan?: number;
  /** 换算后的人民币，4 位小数（mode = cost） */
  yuan?: number;
  /** 成本积分 ⌈yuan × credits_per_yuan⌉（mode = cost） */
  cost?: number;
  /** 加价率（mode = cost） */
  markup_pct?: number;
  /** 调价前的售价（mode = relative） */
  from?: number;
  /** 涨跌百分比，可为负数（mode = relative） */
  pct?: number;
  /** 最终售价（整数积分） */
  price: number;
}

/** 插件变更的展示摘要，代码草稿只随审批 payload 保存 */
export interface PluginDetail {
  /** 插件 key */
  plugin_key: string;
  /** 当前版本，新插件为 null */
  version_before: string | null;
  /** 预计新版本，最终以执行结果为准 */
  version_after: string;
  /** 插件元信息的一行摘要 */
  meta: {
    /** 名称 */
    name: string;
    /** 厂商 */
    vendor?: string;
    /** 鉴权方式 */
    auth: string;
    /** 端点摘要 */
    endpoints: string;
  };
  /** 代码行数 */
  code_lines: number;
  /** unified diff，新插件为空串 */
  code_diff: string;
  /** 最近一次静态验证结论 */
  check: {
    /** 是否通过 */
    ok: boolean;
    /** 渲染出的请求，不含鉴权信息 */
    request?: {
      /** HTTP 方法 */
      method: string;
      /** 请求地址 */
      url: string;
    };
    /** 验证问题 */
    issues: string[];
  };
}

/** 新建渠道的展示摘要，不包含密钥或请求头值 */
export interface ChannelDetail {
  /** 基础地址 */
  base_url: string;
  /** 请求头名字 */
  headers: string[];
  /** 限流设置 */
  rate_limit?: {
    /** 每秒请求数 */
    rps?: number;
    /** 并发数 */
    concurrency?: number;
  };
  /** 密钥状态 */
  secret: "configured" | "missing";
}

/** 新建模型的展示摘要，新建后默认禁用 */
export interface ModelCreateDetail {
  /** 模型种类 */
  kind: string;
  /** 渠道 key */
  channel: string;
  /** 上游模型名 */
  upstream_model: string;
  /** plugin_key@version，版本可为 latest */
  plugin: string;
}

/** 变更单里的一项 */
export interface AdminChangeItem {
  /** 用户或系统管理的具体操作 */
  action?: string;
  /** 改动对象 */
  target:
    /** 修改模型 */
    | "model"
    /** 定价设置 */
    | "pricing_settings"
    /** 上传插件 */
    | "plugin"
    /** 新建渠道 */
    | "channel"
    /** 新建模型 */
    | "model_create"
    /** 用户管理 */
    | "user"
    /** 注册设置 */
    | "register_settings"
    /** 邮件服务 */
    | "smtp_settings"
    /** 存储配置 */
    | "storage"
    /** 图片服务 */
    | "image_processor";
  /** 模型 key；定价设置固定为 "pricing" */
  key: string;
  /** 展示名 */
  label: string;
  /** 目标当前已上线：应用即生效 */
  live: boolean;
  /** ok 可应用；invalid 校验不通过；stale 提案后配置已变；mismatch 价格复算和助手给的不一致 */
  status: "ok" | "invalid" | "stale" | "mismatch";
  /** status 不是 ok 时的原因，中文 */
  issues: string[];
  /** 不阻止应用的提醒 */
  warnings: string[];
  /** 确认这一项需要的角色 */
  required_role: "admin" | "super_admin";
  /** 字段级差异，至少一条 */
  diff: AdminChangeDiff[];
  /** 价格计算依据，不涉及价格时为空数组 */
  basis: AdminPricingBasis[];
  /** 同单内依赖的条目下标，依赖未应用则跳过 */
  depends_on?: number[];
  /** 按 target 展示的新建摘要 */
  detail?: PluginDetail | ChannelDetail | ModelCreateDetail;
}

/** 变更单审批的内容（approval.kind = admin_change） */
export interface AdminChangePayload {
  /** 展示用的变更单号，如 CO-251102-7F3A */
  order_no: string;
  /** 一句话标题 */
  title: string;
  /** 为什么改 */
  reason: string;
  /** 回滚单：被回滚的变更单号 */
  revert_of?: string;
  /** 变更项，至少一项；决定时用下标指代 */
  items: AdminChangeItem[];
}

/** 变更单里一项的执行结果 */
export interface AdminChangeItemResult {
  /** 条目下标 */
  index: number;
  /** 已应用 / 失败 / 跳过 / 被拒绝 */
  status: "applied" | "failed" | "skipped" | "rejected";
  /** failed、skipped 的原因 */
  message?: string;
  /** 实际创建出的版本号或 key */
  created?: {
    /** 插件最终版本 */
    plugin_version?: string;
    /** 渠道 key */
    channel_key?: string;
    /** 模型 key */
    model_key?: string;
  };
}

/** 安全填写渠道密钥的审批内容，绝不含密钥明文 */
export interface AdminSecretPayload {
  /** 渠道 key */
  channel_key: string;
  /** 渠道展示名 */
  channel_label: string;
  /** 渠道是否已经创建 */
  exists: boolean;
  /** 当前是否已经配置密钥 */
  secret_configured: boolean;
  /** 请求配置密钥的原因 */
  reason: string;
  /** 保存密钥需要超级管理员 */
  required_role: "super_admin";
}

/** 密钥审批结果，只含配置状态 */
export interface AdminSecretResult {
  /** 是否已配置密钥 */
  configured: boolean;
}

/** 真实试跑的审批内容，预估费用仅作示意 */
export interface AdminTestRunPayload {
  /** 模型 key */
  model_key: string;
  /** 模型展示名 */
  model_label: string;
  /** 输入的一行摘要 */
  input_summary: string;
  /** 按上游成本估算的费用 */
  estimate: {
    /** 成本积分，未配 cost 为 null */
    credits: number | null;
    /** 人民币金额，无法换算为 null */
    yuan: number | null;
  };
  /** 本运行内第几次试跑，从 1 起 */
  attempt: number;
  /** 剩余试跑次数，上限共 3 次 */
  attempts_left: number;
  /** 试跑原因 */
  reason: string;
  /** 需要管理员权限 */
  required_role: "admin";
}

/** 真实试跑结果，尚未结束时 result 为 null */
export interface AdminTestRunResult {
  /** 试跑任务 ID */
  test_run_id: string;
  /** 成功、失败或超时 */
  status: "succeeded" | "failed" | "timeout";
  /** 失败原因 */
  error?: string;
  /** 首个产物地址 */
  output_url?: string;
  /** 可选扩展：耗时，毫秒；契约尚未定义 */
  duration_ms?: number;
  /** 可选扩展：错误分类；契约尚未定义 */
  error_class?: string;
  /** 可选扩展：trace 地址；契约尚未定义 */
  trace_url?: string;
}

/** 变更单的执行结果（审批的 result 字段，执行前为 null） */
export interface AdminChangeResult {
  /** 逐项结果 */
  items: AdminChangeItemResult[];
  /** 确认人的用户名 */
  applied_by: string;
  /** 执行时间，ISO 8601 */
  applied_at: string;
  /** 写入的审计记录 id，每个已应用的条目一条 */
  audit_ids: number[];
}

/** 审批（后端返回的原始结构，payload 按种类解析前是 JSON） */
export interface AgentApprovalDto {
  /** 审批 ID */
  id: string;
  /** 所属运行 */
  run_id: string;
  /** 审批种类 */
  kind: AgentApprovalKind;
  /** 审批状态 */
  status: AgentApprovalStatus;
  /** 待决定的内容，形状由 kind 决定：AgentGeneratePayload / AgentDeletePayload / AgentAskPayload */
  payload:
    | AgentGeneratePayload
    | AgentDeletePayload
    | AgentAskPayload
    | AgentEditPayload
    | AdminChangePayload
    | AdminSecretPayload
    | AdminTestRunPayload;
  /** 预估积分 */
  quote_credits: number;
  /** 用户的决定，未决定为 null */
  decision: Record<string, unknown> | null;
  /** 执行结果：admin_change 是 AdminChangeResult；执行前、旧服务没有这个字段，为 null 或缺省 */
  result?:
    | AdminChangeResult
    | AdminSecretResult
    | AdminTestRunResult
    | Record<string, unknown>
    | null;
  /** 过期时间 */
  expires_at: string;
}

/** 事件（HTTP 回放与 WebSocket 推送共用） */
export interface AgentEventDto {
  /** 所属会话 */
  session_id: string;
  /** 所属画布：推送走用户频道，前端靠它过滤 */
  canvas_id: string;
  /** 所属运行，会话级事件为 null */
  run_id: string | null;
  /** 会话内递增序号；0 表示临时事件（文本、思考增量），不参与回放 */
  seq: number;
  /** 事件类型 */
  type: string;
  /** 事件内容，按类型不同 */
  data: Record<string, unknown>;
  /** 创建时间 */
  created_at: string;
}

/** 后台等待里一个生成任务的最终结果（watch.updated / tasks.settled 里的 task_status） */
export type AgentWatchTaskResult = "succeeded" | "failed" | "expired" | "canceled" | "missing";

/** 后台等待里的一个生成任务 */
export interface AgentWatchTaskDto {
  /** 生成任务 ID（十进制字符串） */
  task_id: string;
  /** 它绑定的画布节点 */
  node_id: string;
  /** 节点标题，批准时记下的 */
  label: string;
  /** pending 还没结束；settled 已结束，结果看 task_status */
  status: "pending" | "settled";
  /** 结束后的结果，还没结束没有 */
  task_status?: AgentWatchTaskResult;
}

/**
 * 一批后台等待：一次批准的生成。waiting 还有任务没结束；deliverable 全部结束、等当前运行结束后 Agent 继续；
 * delivered 已交给 Agent；canceled 用户点了「不用等了」
 */
export interface AgentWatchBatchDto {
  /** 批 ID（批准的审批 ID） */
  batch_id: string;
  /** 这一批的状态 */
  state: "waiting" | "deliverable" | "delivered" | "canceled";
  /** 这一批的任务总数 */
  total: number;
  /** 已结束的任务数 */
  settled: number;
  /** 逐个任务 */
  tasks: AgentWatchTaskDto[];
}

/** 一页聊天记录：事件按序号升序，has_more 表示更早还有没加载的 */
export type AgentHistoryDto = {
  events: AgentEventDto[];
  has_more: boolean;
};

/** Agent 改画布后推送的改动（canvas.patch） */
export interface CanvasPatchDto {
  /** 改动 ID */
  mutation_id: string;
  /** 所属运行 */
  run_id: string;
  /** 所属画布 */
  canvas_id: string;
  /** 改动种类：apply_ops / arrange / delete / bind / undo / frames */
  kind: string;
  /** 改动前的画布 revision */
  revision_before: number;
  /** 改动后的画布 revision */
  revision_after: number;
  /** 逐节点、逐连线的改动 */
  changes: GraphChange[];
}

/** 后台助手每次发送带上的页面上下文（只对 admin 会话生效） */
export interface AdminPageContext {
  /** 当前后台路由，如 /admin/ai/channels */
  route: string;
  /** 当前选中的模型 */
  model_key?: string;
  /** 当前选中的渠道 */
  channel_key?: string;
  /** 当前选中的插件 */
  plugin_key?: string;
}

/** 新建后台助手会话 */
export interface CreateAdminAssistantSessionReq {
  /** 标题，不传由服务端命名 */
  title?: string;
  /** Agent 模型 key，不传取第一个可用的 */
  model_key?: string;
}

/** 新建会话 */
export interface CreateAgentSessionReq {
  /** 标题，不传默认「新对话」 */
  title?: string;
  /** 任务模式，不传默认 all */
  mode?: AgentMode;
  /** Agent 模型 key，不传取第一个可用的 */
  model_key?: string;
}

/** 随用户消息发出的图片附件（请求里只带素材 id） */
export interface AgentAttachmentReq {
  /** 素材 ID（先用 uploadAsset 上传得到） */
  asset_id: string;
}

/** 用户消息事件（message.user）里记录的附件（图片或文档）：只存引用和展示信息，不存内容 */
export interface AgentAttachmentDto {
  /** 素材 ID */
  asset_id: string;
  /** 展示名 */
  name: string;
  /** MIME 类型：图片 png / jpeg / webp / gif，文档 text/plain / text/markdown */
  mime_type: string;
  /** 种类；旧事件没有，按图片处理 */
  kind?: "image" | "document";
  /** 文件大小（字节）；文档卡片显示用 */
  byte_size?: number;
  /** 宽高（像素），没有为 0 或缺省 */
  width?: number;
  height?: number;
  /** 素材的稳定访问地址（/files/*，不会过期） */
  url: string;
}

/** 发起一轮运行 */
export interface StartAgentRunReq {
  /** 用户的消息，行内 chip 序列化为 @[名字](类型:id) */
  message: string;
  /** 任务模式，不传沿用会话的 */
  mode?: AgentMode;
  /** 选中的节点 id。前端已不再传（选中节点改成输入框里的 node: chip），字段保留给旧客户端 */
  selection?: string[];
  /** 视口 */
  viewport?: { x: number; y: number; zoom: number };
  /** Agent 模型 key，不传沿用会话的 */
  agent_model_key?: string;
  /** 图片附件，最多 9 张；当前模型不能看图时不能带 */
  attachments?: AgentAttachmentReq[];
  /** 页面上下文，只对后台助手会话生效，画布会话忽略 */
  page_context?: AdminPageContext;
}

/** 对审批里某一项的决定 */
export interface ApprovalItemDecision {
  /** 条目下标 */
  index: number;
  /** 是否批准这一项 */
  approve: boolean;
  /** 生成个数，只能不大于申请的；0 表示按申请的 */
  count?: number;
}

/** 对审批的决定 */
export interface DecideAgentApprovalReq {
  /** approve 或 reject */
  decision: "approve" | "reject";
  /** 逐项决定，不传表示整体 */
  items?: ApprovalItemDecision[];
  /** 提问的回答；拒绝时是给 Agent 的说明（「拒绝，并告诉 Agent 怎么改」） */
  answer?: string;
  /** 提问逐题的回答，选填题不答可省略；value 多选题是字符串数组 */
  answers?: Array<{ id: string; value: string | string[] }>;
}

/** 撤销本轮的结果 */
export interface AgentUndoResult {
  /** 恢复或删除了多少项 */
  reverted: number;
  /** 没处理的项和原因（用户之后改过） */
  skipped: Array<{ id: string; field?: string; reason: string }>;
  /** 撤销后的画布 revision，没有写入时为 0 */
  revision: number;
}

/** @ 弹层里的一个技能：已启用的内置技能与导入技能 */
export interface AgentSkillDto {
  /** 技能名，Agent 靠它 skill_read，也是 chip 的 id */
  name: string;
  /** 显示名，chip 上显示 */
  title: string;
  /** 一句话说明 */
  description: string;
  /** 来源：builtin 内置 / imported 导入 */
  source: "builtin" | "imported";
}

/** 对一轮运行的评价：1 点赞，-1 点踩 */
export type AgentFeedbackRating = 1 | -1;

/** 点踩的原因标签 */
export type AgentFeedbackReason =
  /** 没理解我的需求 */
  | "misread"
  /** 画布改错了 */
  | "wrong_edit"
  /** 步骤太多或太慢 */
  | "slow"
  /** 生成结果不好 */
  | "bad_output"
  /** 其他 */
  | "other";

/** 一轮运行的反馈（接口返回与事件内容共用的形状） */
export interface AgentFeedbackDto {
  /** 评价：1 点赞，-1 点踩 */
  rating: AgentFeedbackRating;
  /** 原因标签，点赞或没选为空数组 */
  reasons: AgentFeedbackReason[];
  /** 补充说明，没有为空串，最多 500 字 */
  comment: string;
}

/** 提交一轮运行的反馈（再次提交即覆盖） */
export interface SetAgentFeedbackReq {
  /** 评价：1 点赞，-1 点踩 */
  rating: AgentFeedbackRating;
  /** 原因标签，可不传 */
  reasons?: AgentFeedbackReason[];
  /** 补充说明，可不传，最多 500 字 */
  comment?: string;
}
