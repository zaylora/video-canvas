/**
 * 对外的视图：前端看到的 JSON。字段名、编码方式和 Go 的 AgentSessionView / AgentRunView / AgentEventView 完全一致
 * （id 是 idcodec 编码串，时间是 ISO，未结束的结束时间是 null），由黄金文件测试对照。
 */
import type { IdCodec } from "../ids.ts";
import type { AgentEventRecord, Run, Session } from "../store/types.ts";

const iso = (ms: number): string => new Date(ms).toISOString();

export interface SessionView {
  id: string;
  canvas_id: string;
  title: string;
  mode: string;
  model_key: string;
  last_seq: number;
  created_at: string;
  updated_at: string;
}

export function sessionView(c: IdCodec, s: Session): SessionView {
  return {
    id: c.encodeDecimal(s.id), canvas_id: c.encodeDecimal(s.canvasId), title: s.title, mode: s.mode, model_key: s.modelKey,
    last_seq: s.lastSeq, created_at: iso(s.createdAt), updated_at: iso(s.updatedAt),
  };
}

export interface RunView {
  id: string;
  session_id: string;
  canvas_id: string;
  status: string;
  mode: string;
  budget_credits: number;
  spent_credits: number;
  context_tokens: number;
  steps: number;
  max_steps: number;
  error_code: string;
  error_message: string;
  created_at: string;
  ended_at: string | null;
}

export function runView(c: IdCodec, r: Run): RunView {
  return {
    id: c.encodeDecimal(r.id), session_id: c.encodeDecimal(r.sessionId), canvas_id: c.encodeDecimal(r.canvasId), status: r.status, mode: r.mode,
    budget_credits: r.budgetCredits, spent_credits: r.spentCredits, context_tokens: r.contextTokens, steps: r.steps, max_steps: r.maxSteps,
    error_code: r.errorCode, error_message: r.errorMessage, created_at: iso(r.createdAt), ended_at: r.endedAt === null ? null : iso(r.endedAt),
  };
}

export interface EventView {
  session_id: string;
  /** HTTP 回放里没有（null）：推送走用户频道，前端靠推送里的画布 id 过滤；回放按会话取，不需要 */
  canvas_id: string | null;
  run_id: string | null;
  seq: number;
  type: string;
  data: unknown;
  created_at: string;
}

export function eventView(c: IdCodec, e: AgentEventRecord, canvasId?: string): EventView {
  return {
    session_id: c.encodeDecimal(e.sessionId), canvas_id: canvasId ? c.encodeDecimal(canvasId) : null,
    run_id: e.runId === "0" ? null : c.encodeDecimal(e.runId), seq: e.seq, type: e.type, data: e.payload, created_at: iso(e.createdAt),
  };
}

export interface HistoryView {
  events: EventView[];
  has_more: boolean;
}

// ---- 观测（给 Go 的后台读，内部接口）----
// 和面向前端的视图不同：id 一律是十进制字符串（编码串只在 Go 手里），事件带 run_id 好区分上下文属于哪一轮。

export interface ObserveRunRef {
  id: string;
  status: string;
  /** 毫秒时间戳；Go 用它做同步游标 */
  updated_at: number;
}

export interface ObserveEvent {
  run_id: string;
  seq: number;
  type: string;
  data: unknown;
  created_at: string;
}

export interface ObserveApproval {
  id: string;
  kind: string;
  tool_call_id: string;
  status: string;
  quote_credits: number;
  payload: unknown;
  decision: unknown;
  result: unknown;
  decided_at: string | null;
  created_at: string;
}

export interface ObserveFacts {
  run: {
    id: string; session_id: string; canvas_id: string; user_id: string; status: string; mode: string;
    budget_credits: number; spent_credits: number; context_tokens: number; steps: number; max_steps: number;
    error_code: string; error_message: string; prompt_version: number; tools_hash: string; build_sha: string; harness_key: string;
    created_at: string; ended_at: string | null;
  };
  session_deleted: boolean;
  /** 本轮的全部落库事件，按序号升序 */
  events: ObserveEvent[];
  approvals: ObserveApproval[];
  feedback: { rating: number; reasons: string[]; comment: string; created_at: string } | null;
  /** 同会话更早两轮的用户消息和最终回复，按序号升序 */
  context: ObserveEvent[];
}

export function observeEvent(e: AgentEventRecord): ObserveEvent {
  return { run_id: e.runId, seq: e.seq, type: e.type, data: e.payload, created_at: iso(e.createdAt) };
}
