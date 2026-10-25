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
