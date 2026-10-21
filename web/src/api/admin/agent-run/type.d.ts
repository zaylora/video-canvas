/**
 * 运行状态。已知值：queued 排队、running 运行中、waiting_approval 等审批、waiting_input 等回答、
 * succeeded 完成、failed 失败、canceled 用户停止、budget_exhausted 预算用尽、step_limit 步数上限、
 * timeout 超时、interrupted 被中断、expired 等待过久。后端新增状态时页面按中性色兜底，所以保持 string
 */
export type AgentRunStatus = string;

/** 一次运行的摘要（列表每行；详情里的 digest 也是它）。ID 类字段是后端编码后的 32 位十六进制串 */
export interface AgentRunDigest {
  /** 运行 ID（编码串），详情接口的 :rid */
  run_id: string;
  /** 会话 ID（编码串） */
  session_id: string;
  /** 画布 ID（编码串） */
  canvas_id: string;
  /** 用户 ID（数字，后台用，不编码） */
  user_id: number;
  /** 任务模式 */
  mode: string;
  /** Agent 模型 key */
  model_key: string;
  /** 系统提示词版本 */
  prompt_version: number;
  /** 工具声明哈希 */
  tools_hash: string;
  /** 构建的 git commit */
  build_sha: string;
  /** Harness 版本短哈希（提示词 + 工具声明 + 技能等的指纹） */
  harness_key: string;
  /** 本次运行读过的技能：技能名 → 版本（"v3" 或 "builtin"） */
  skills: Record<string, string>;
  /** 运行状态 */
  status: AgentRunStatus;
  /** 失败时的错误码，没有为空串 */
  error_code: string;
  /** 用户请求的前 200 字 */
  request_head: string;
  /** 步数 */
  steps: number;
  /** 工具调用次数 */
  tool_calls: number;
  /** 工具报错次数 */
  tool_errors: number;
  /** 模型调用次数 */
  model_calls: number;
  /** 输入 Token */
  input_tokens: number;
  /** 输出 Token */
  output_tokens: number;
  /** 花掉的积分 */
  spent_credits: number;
  /** 耗时（毫秒） */
  duration_ms: number;
  /** 审批总数 */
  approvals_total: number;
  /** 被用户拒绝的审批数 */
  approvals_rejected: number;
  /** 用户插话次数 */
  steer_count: number;
  /** 改画布的次数 */
  mutation_count: number;
  /** 改动被用户撤销过 */
  undone: boolean;
  /** 用户评价：1 有帮助，-1 没帮上忙，0 没评价 */
  feedback: 1 | -1 | 0;
  /** 结束时间（RFC3339），未结束为 null */
  ended_at: string | null;
  /** 运行创建时间（RFC3339） */
  created_at: string;
  /** 摘要更新时间（RFC3339） */
  updated_at: string;
}

/** 列表的反馈筛选：down 点踩、up 点赞、none 没评价、any 有评价；不传表示不限 */
export type AgentRunFeedbackFilter = "down" | "up" | "none" | "any";

/** 运行列表的查询参数；全部可选，没填的字段不会发给后端 */
export interface AgentRunListQuery {
  /** 起（含），RFC3339 或 YYYY-MM-DD */
  since?: string;
  /** 止（不含），RFC3339 或 YYYY-MM-DD */
  until?: string;
  /** 状态，逗号分隔多个 */
  status?: string;
  /** 反馈筛选 */
  feedback?: AgentRunFeedbackFilter;
  /** 只要被撤销过的；只能传 true，不筛选就不传 */
  undone?: true;
  /** Harness 版本 */
  harness_key?: string;
  /** 系统提示词版本 */
  prompt_version?: number;
  /** Agent 模型 */
  model_key?: string;
  /** 用户 ID */
  user_id?: number;
  /** 上一页返回的 next_cursor；第一页不传 */
  cursor?: string;
  /** 每页条数，最大 500 */
  limit?: number;
}

/** 运行列表的一页 */
export interface AgentRunPage {
  /** 本页记录，创建时间倒序 */
  items: AgentRunDigest[];
  /** 下一页游标；空串表示没有下一页 */
  next_cursor: string;
}

/** 时间线步骤的种类，前端据此决定图标和颜色 */
export type AgentRunStepKind =
  /** 用户消息 */
  | "user"
  /** 用户插话 */
  | "steer"
  /** 模型回复 */
  | "assistant"
  /** 工具调用 */
  | "tool"
  /** 审批请求和结果 */
  | "approval"
  /** 计划更新 */
  | "plan"
  /** 用户撤销本轮 */
  | "undo"
  /** 用户反馈 */
  | "feedback"
  /** 运行时提示 */
  | "notice"
  /** 运行状态 */
  | "status"
  /** 其他事件 */
  | "other";

/** 时间线里工具调用这一步的细节 */
export interface AgentRunStepTool {
  /** 工具名 */
  name: string;
  /** 耗时（毫秒）；没有记录时缺省 */
  duration_ms?: number;
  /** 工具没做成 */
  is_error: boolean;
  /** 一行摘要 */
  summary?: string;
  /** 参数（压缩 JSON，已截到 2KB） */
  args?: string;
  /** 返回给模型的文本（已截到 2KB） */
  result?: string;
  /** 参数或结果被截断过，完整内容在导出 JSON 的 tool_calls 里 */
  truncated?: boolean;
  /** 有这次调用的记录；false 表示没有记录（落库失败，或运行早于观测上线） */
  recorded: boolean;
  /** 参数和结果已过保留期被清理的时间（RFC3339） */
  purged_at?: string;
}

/** 时间线的一步：Markdown 导出和后台抽屉由后端用同一份步骤列表生成，前端不要自己再拼 */
export interface AgentRunStep {
  /** 步骤编号，分析时用 #n 引用证据 */
  n: number;
  /** 种类 */
  kind: AgentRunStepKind;
  /** 这一步的名称，如「用户」「模型回复」「工具调用」 */
  label: string;
  /** 正文（已按 2KB 截断）；工具调用为空，内容在 tool 里 */
  text: string;
  /** 模型回复的思考摘要，前 600 字 */
  thinking?: string;
  /** 半截回答，被中断 */
  partial?: boolean;
  /** codemode 脚本里发起的子调用 */
  nested?: boolean;
  /** 事件时间（RFC3339） */
  at?: string;
  /** 工具调用的细节，只有 kind 为 tool 时有 */
  tool?: AgentRunStepTool;
}

/** 一次工具调用的完整记录（参数和结果不截断，过保留期后为 null） */
export interface AgentRunToolCall {
  /** 模型给的调用 ID */
  id: string;
  /** codemode 子调用指向脚本那次调用的 ID，顶层调用为空串 */
  parent_id: string;
  /** 工具名 */
  name: string;
  /** 调用参数；过保留期后为 null */
  args: unknown;
  /** 返回给模型的文本；过保留期后为 null */
  result: string | null;
  /** 结果在落库时被截断过 */
  result_truncated: boolean;
  /** 工具没做成 */
  is_error: boolean;
  /** 一行摘要 */
  summary: string;
  /** 开始时间（RFC3339） */
  started_at: string;
  /** 结束时间（RFC3339），未结束为 null */
  ended_at: string | null;
  /** 耗时（毫秒） */
  duration_ms: number;
  /** 参数和结果已过保留期被清理 */
  purged: boolean;
}

/** 一次模型调用 */
export interface AgentRunModelCall {
  /** 用途：main 对话，summary 摘要 */
  purpose: string;
  /** Agent 模型 key */
  model_key: string;
  /** 实际走的渠道 key */
  channel_key: string;
  /** 实际调用的上游模型 */
  upstream_model: string;
  /** 输入 Token */
  input_tokens: number;
  /** 输出 Token */
  output_tokens: number;
  /** 命中缓存的输入 Token */
  cached_tokens: number;
  /** Token 数是估算的 */
  usage_estimated: boolean;
  /** 应收积分 */
  credits: number;
  /** 实扣积分 */
  charged: number;
  /** 总耗时（毫秒） */
  latency_ms: number;
  /** 首字耗时（毫秒），0 表示没有 */
  ttft_ms: number;
  /** 上游的结束原因 */
  stop_reason: string;
  /** 失败或中断的原因，没有为空串 */
  error: string;
  /** 发起时间（RFC3339） */
  created_at: string;
}

/** 一次画布改动 */
export interface AgentRunMutation {
  /** 运行内序号 */
  seq: number;
  /** 触发它的工具调用 ID */
  tool_call_id: string;
  /** 改动种类 */
  kind: string;
  /** 改动明细，通常是数组；缺失时为 null */
  changes: unknown;
  /** 写入前的画布版本 */
  revision_before: number;
  /** 写入后的画布版本 */
  revision_after: number;
  /** 被撤销的时间（RFC3339），没撤销为 null */
  undone_at: string | null;
}

/** 一次审批 */
export interface AgentRunApproval {
  /** 审批 ID（编码串） */
  id: string;
  /** 审批种类 */
  kind: string;
  /** 触发它的工具调用 ID */
  tool_call_id: string;
  /** 状态 */
  status: string;
  /** 预估积分 */
  quote_credits: number;
  /** 待决定的内容 */
  payload: unknown;
  /** 用户的决定 */
  decision: unknown;
  /** 执行结果 */
  result: unknown;
  /** 决定时间（RFC3339），未决定为 null */
  decided_at: string | null;
}

/** 点踩的原因标签 */
export type AgentRunFeedbackReason = "misread" | "wrong_edit" | "slow" | "bad_output" | "other";

/** 用户对一次运行的评价 */
export interface AgentRunFeedback {
  /** 1 有帮助，-1 没帮上忙 */
  rating: 1 | -1;
  /** 点踩原因标签；后端新增标签时按原文显示，所以是 string */
  reasons: string[];
  /** 补充说明原文 */
  comment: string;
}

/** 落库事件（context 里是同会话更早两轮的用户消息和最终回复） */
export interface AgentRunEvent {
  /** 所属会话（编码串） */
  session_id: string;
  /** 所属画布（编码串） */
  canvas_id: string;
  /** 所属运行（编码串），会话级事件为 null */
  run_id: string | null;
  /** 会话内递增序号 */
  seq: number;
  /** 事件类型，如 message.user、message.done */
  type: string;
  /** 事件内容；消息类事件里有 text */
  data: Record<string, unknown> | null;
  /** 创建时间（RFC3339） */
  created_at: string;
}

/** 运行本身 */
export interface AgentRunInfo {
  /** 运行 ID（编码串） */
  id: string;
  /** 所属会话（编码串） */
  session_id: string;
  /** 所属画布（编码串） */
  canvas_id: string;
  /** 运行状态 */
  status: AgentRunStatus;
  /** 任务模式 */
  mode: string;
  /** 本轮积分预算 */
  budget_credits: number;
  /** 已花积分 */
  spent_credits: number;
  /** 已执行步数 */
  steps: number;
  /** 步数上限 */
  max_steps: number;
  /** 失败时的错误码 */
  error_code: string;
  /** 失败时给用户看的文案 */
  error_message: string;
  /** 创建时间（RFC3339） */
  created_at: string;
  /** 结束时间（RFC3339），未结束为 null */
  ended_at: string | null;
}

/** 一次运行的详情，也是「下载 JSON」文件的内容 */
export interface AgentRunDetail {
  /** 运行摘要；运行刚创建还没有摘要时为 null */
  digest: AgentRunDigest | null;
  /** 运行本身 */
  run: AgentRunInfo | null;
  /** 时间线步骤，页面渲染以它为准 */
  timeline: AgentRunStep[];
  /** 本轮的落库事件，按序号升序 */
  events: AgentRunEvent[];
  /** 工具调用，含完整参数和结果 */
  tool_calls: AgentRunToolCall[];
  /** 模型调用 */
  model_calls: AgentRunModelCall[];
  /** 画布改动 */
  mutations: AgentRunMutation[];
  /** 审批 */
  approvals: AgentRunApproval[];
  /** 用户评价；没评价为 null */
  feedback: AgentRunFeedback | null;
  /** 同会话更早两轮的用户消息和最终回复 */
  context: AgentRunEvent[];
  /** Node 进程 stderr 末尾；过保留期后为空串 */
  runtime_tail: string;
}

/** 导出接口的返回 */
export interface AgentRunExport {
  /** 导出格式，目前只有 md */
  format: "md";
  /** Markdown 正文 */
  content: string;
}

/** 统计的分组维度 */
export type AgentStatsGroupBy = "harness_key" | "model_key";

/** 统计接口的查询参数 */
export interface AgentStatsQuery {
  /** 分组维度 */
  group_by: AgentStatsGroupBy;
  /** 起（含），RFC3339 */
  since?: string;
  /** 止（不含），RFC3339 */
  until?: string;
}

/** 一组的聚合指标；比率都是 0–1 的小数，分母为 0 时后端记 0 */
export interface AgentStatsGroup {
  /** 分组值（harness_key 或 model_key） */
  key: string;
  /** 运行数 */
  n: number;
  /** 有评价的运行占比 */
  feedback_rate: number;
  /** 点踩率：点踩数 ÷ 有评价的运行数 */
  down_rate: number;
  /** 撤销率：被撤销的运行数 ÷ 改过画布的运行数 */
  undo_rate: number;
  /** 失败结束率：失败、超时、中断、步数或预算用尽的运行占比 */
  fail_end_rate: number;
  /** 耗时中位数（毫秒） */
  median_duration_ms: number;
  /** 步数中位数 */
  median_steps: number;
  /** 平均每次运行花的积分 */
  avg_credits: number;
  /** 这一组的提示词版本（按 harness_key 分组时唯一） */
  prompt_version: number;
  /** 这一组的构建号 */
  build_sha: string;
  /** 这一组最近一次运行的创建时间（RFC3339），分组按它倒序 */
  last_at: string;
}

/** 统计接口的返回 */
export interface AgentStatsResult {
  /** 各组指标，按最近一次运行时间倒序 */
  groups: AgentStatsGroup[];
}
