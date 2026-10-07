import type { GraphChange } from "@/utils/canvas/graph-changes";

/** 任务模式：全能创作 / 剧本创编 / 分镜搭建 / 提示词优化 */
export type AgentMode = "all" | "script" | "storyboard" | "prompt";

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
  /** 完成 */
  | "succeeded"
  /** 失败 */
  | "failed"
  /** 用户停止 */
  | "canceled"
  /** 本轮积分预算用尽，暂停，可继续 */
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
  /** 是否支持看图 */
  vision: boolean;
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
  /** 本轮积分预算 */
  budget_credits: number;
  /** 已花积分 */
  spent_credits: number;
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

/** 审批种类：生成 / 删除 / 提问 */
export type AgentApprovalKind = "generate" | "delete" | "ask";

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

/** 提问的内容（approval.kind = ask） */
export interface AgentAskPayload {
  /** 问题 */
  question: string;
  /** choice 选项题；model 选模型题 */
  kind: "choice" | "model";
  /** 选项（choice） */
  options?: string[];
  /** 要选哪类模型（model） */
  model_kind?: "image" | "video";
  /** 是否允许自定义回答 */
  allow_custom?: boolean;
  /** 可选的已发布模型（model） */
  models?: Array<{ key: string; name: string; hint: string }>;
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
  payload: AgentGeneratePayload | AgentDeletePayload | AgentAskPayload;
  /** 预估积分 */
  quote_credits: number;
  /** 用户的决定，未决定为 null */
  decision: Record<string, unknown> | null;
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

/** Agent 改画布后推送的改动（canvas.patch） */
export interface CanvasPatchDto {
  /** 改动 ID */
  mutation_id: string;
  /** 所属运行 */
  run_id: string;
  /** 所属画布 */
  canvas_id: string;
  /** 改动种类：apply_ops / arrange / delete / bind / undo */
  kind: string;
  /** 改动前的画布 revision */
  revision_before: number;
  /** 改动后的画布 revision */
  revision_after: number;
  /** 逐节点、逐连线的改动 */
  changes: GraphChange[];
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

/** 发起一轮运行 */
export interface StartAgentRunReq {
  /** 用户的消息，行内 chip 序列化为 @[名字](类型:id) */
  message: string;
  /** 任务模式，不传沿用会话的 */
  mode?: AgentMode;
  /** 选中的节点 id */
  selection?: string[];
  /** 视口 */
  viewport?: { x: number; y: number; zoom: number };
  /** 本轮积分预算，不传默认 50 */
  budget_credits?: number;
  /** Agent 模型 key，不传沿用会话的 */
  agent_model_key?: string;
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
  /** 提问的回答 */
  answer?: string;
  /** 批准时追加的积分预算 */
  add_budget?: number;
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
