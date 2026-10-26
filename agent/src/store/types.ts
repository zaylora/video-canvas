/**
 * Agent 服务的领域对象。ID 一律是十进制字符串（雪花 ID 超过 2^53），时间一律是毫秒时间戳。
 * 字段含义和 Go 的 model.AgentSession / AgentRun / AgentEvent / AgentApproval 对应；会话历史不在这里，归 Pi Durable。
 */
export type TitleSource = "draft" | "llm" | "user";

export interface Session {
  id: string;
  userId: string;
  canvasId: string;
  title: string;
  /** 标题是谁起的：draft 草稿（首条消息截出来的）、llm 模型命名、user 用户手动起的 */
  titleSource: TitleSource;
  mode: string;
  modelKey: string;
  /** 最近一条事件的序号 */
  lastSeq: number;
  /** 会话在 Pi Durable 里的会话 id；还没开始过对话为空串 */
  conversationId: string;
  createdAt: number;
  updatedAt: number;
}

export interface Run {
  id: string;
  sessionId: string;
  canvasId: string;
  userId: string;
  status: string;
  mode: string;
  budgetCredits: number;
  spentCredits: number;
  contextTokens: number;
  steps: number;
  maxSteps: number;
  errorCode: string;
  errorMessage: string;
  promptVersion: number;
  toolsHash: string;
  buildSha: string;
  harnessKey: string;
  leaseUntil: number;
  createdAt: number;
  updatedAt: number;
  endedAt: number | null;
}

export interface AgentEventRecord {
  id: string;
  sessionId: string;
  /** 所属运行，会话级事件为 "0" */
  runId: string;
  seq: number;
  type: string;
  payload: unknown;
  createdAt: number;
}

export interface Approval {
  id: string;
  runId: string;
  sessionId: string;
  canvasId: string;
  userId: string;
  kind: string;
  toolCallId: string;
  payload: unknown;
  quoteCredits: number;
  status: string;
  decision: unknown;
  result: unknown;
  decidedAt: number | null;
  expiresAt: number;
  createdAt: number;
  updatedAt: number;
}

export interface NewSession {
  userId: string;
  canvasId: string;
  title: string;
  /** 不传是草稿 */
  titleSource?: TitleSource;
  mode: string;
  modelKey: string;
}

export interface NewRun {
  sessionId: string;
  canvasId: string;
  userId: string;
  status: string;
  mode: string;
  budgetCredits?: number;
  maxSteps?: number;
  promptVersion?: number;
  toolsHash?: string;
  buildSha?: string;
  harnessKey?: string;
  leaseUntil?: number;
}

export interface NewApproval {
  runId: string;
  sessionId: string;
  canvasId: string;
  userId: string;
  kind: string;
  toolCallId: string;
  payload: unknown;
  quoteCredits: number;
  status: string;
  expiresAt: number;
}

export interface SessionPatch {
  title?: string;
  titleSource?: TitleSource;
  mode?: string;
  modelKey?: string;
}

/** 状态迁移时可以一并更新的运行字段 */
export interface RunPatch {
  status?: string;
  errorCode?: string;
  errorMessage?: string;
  endedAt?: number | null;
  leaseUntil?: number;
  mode?: string;
  budgetCredits?: number;
  spentCredits?: number;
  steps?: number;
  contextTokens?: number;
  maxSteps?: number;
  promptVersion?: number;
  toolsHash?: string;
  buildSha?: string;
  harnessKey?: string;
}

export interface ApprovalPatch {
  status?: string;
  decision?: unknown;
  result?: unknown;
  decidedAt?: number | null;
  quoteCredits?: number;
}

export interface Feedback {
  runId: string;
  userId: string;
  /** 1 有帮助，-1 没帮上忙 */
  rating: number;
  reasons: string[];
  comment: string;
  createdAt: number;
  updatedAt: number;
}

export interface NewFeedback {
  runId: string;
  userId: string;
  rating: number;
  reasons: string[];
  comment: string;
}
