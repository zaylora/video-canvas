import type { AgentApprovalDto, AgentEventDto, AgentRunStatus } from "@/api/agent/type";

/** 计划里的一步 */
export type AgentPlanStep = {
  /** 这一步做什么 */
  title: string;
  /** 待做 / 进行中 / 已完成 */
  status: "todo" | "doing" | "done";
};

/** 一轮运行的实时状态，由 run.status 事件推出来 */
export type AgentRunLive = {
  /** 运行状态 */
  status: AgentRunStatus;
  /** 失败或暂停时给用户看的原因，没有为空串 */
  error: string;
  /** 本轮已花积分（run.usage 或运行接口给出），还不知道时没有 */
  spent?: number;
  /** 本轮积分预算，还不知道时没有 */
  budget?: number;
};

/** 正在流式输出、还没落成 message.done 的文字 */
export type AgentStream = {
  /** 所属运行 */
  runId: string | null;
  /** 已收到的回复文字 */
  text: string;
  /** 已收到的思考文字 */
  thinking: string;
};

/** 一个会话在前端的状态 */
export type AgentSessionState = {
  /** 持久化事件（seq > 0），按序号升序，没有重复 */
  events: AgentEventDto[];
  /** 已收到的最大序号 */
  lastSeq: number;
  /** 从 1 开始连续收齐到的最大序号：回放从它之后拉，中间漏掉的会补上 */
  contiguousSeq: number;
  /** 流式输出中的文字 */
  stream: AgentStream;
  /** 各轮运行的实时状态 */
  runs: Record<string, AgentRunLive>;
  /** 各个审批的最新状态（含提问） */
  approvals: Record<string, AgentApprovalDto>;
  /** 各轮运行最新的计划 */
  plans: Record<string, AgentPlanStep[]>;
};

/** 运行还占着画布的状态：Agent 在跑或在等用户 */
export const ACTIVE_RUN_STATUSES: readonly AgentRunStatus[] = [
  "queued",
  "running",
  "waiting_approval",
  "waiting_input",
];

/** 空会话 */
export const emptySession = (): AgentSessionState => ({
  events: [],
  lastSeq: 0,
  contiguousSeq: 0,
  stream: { runId: null, text: "", thinking: "" },
  runs: {},
  approvals: {},
  plans: {},
});

const asString = (v: unknown) => (typeof v === "string" ? v : "");

/** 把一条持久化事件折进衍生状态（运行状态、审批、计划、流式文字） */
function foldDerived(state: AgentSessionState, ev: AgentEventDto): AgentSessionState {
  const data = ev.data ?? {};
  switch (ev.type) {
    case "run.status": {
      if (!ev.run_id) return state;
      const status = asString(data.status) as AgentRunStatus;
      const prev = state.runs[ev.run_id];
      const runs = {
        ...state.runs,
        [ev.run_id]: { ...prev, status, error: asString(data.error) },
      };
      // 运行不再进行了，没落成 message.done 的残余文字不再有意义
      const stream = ACTIVE_RUN_STATUSES.includes(status)
        ? state.stream
        : { runId: null, text: "", thinking: "" };
      return { ...state, runs, stream };
    }
    case "run.usage": {
      // 只有已经知道的运行才记用量：用量事件不该凭空造出一轮运行
      const prev = ev.run_id ? state.runs[ev.run_id] : undefined;
      if (!ev.run_id || !prev) return state;
      const spent = typeof data.spent_credits === "number" ? data.spent_credits : prev.spent;
      const budget = typeof data.budget_credits === "number" ? data.budget_credits : prev.budget;
      return { ...state, runs: { ...state.runs, [ev.run_id]: { ...prev, spent, budget } } };
    }
    case "approval.created":
    case "approval.decided": {
      const approval = data as unknown as AgentApprovalDto;
      if (!approval.id) return state;
      return { ...state, approvals: { ...state.approvals, [approval.id]: approval } };
    }
    case "plan.updated": {
      if (!ev.run_id || !Array.isArray(data.steps)) return state;
      return { ...state, plans: { ...state.plans, [ev.run_id]: data.steps as AgentPlanStep[] } };
    }
    case "message.done":
      return { ...state, stream: { runId: null, text: "", thinking: "" } };
    default:
      return state;
  }
}

/**
 * 把一条事件并进会话状态，不改入参。
 * - 临时事件（seq 为 0）：只累加流式文字，不进事件列表；message.done 会带着完整内容把它清掉
 * - 持久化事件：按序号去重、插入到正确的位置（回放和实时推送可能交错），并折进衍生状态；
 *   实时推送乱序到达时，衍生状态以序号大的为准（回放补来的旧事件不会把新状态倒回去）
 */
export function applyAgentEvent(state: AgentSessionState, ev: AgentEventDto): AgentSessionState {
  if (ev.seq === 0) {
    const data = ev.data ?? {};
    if (ev.type === "message.delta") {
      const stream = {
        runId: ev.run_id,
        text: state.stream.text + asString(data.text),
        thinking: state.stream.thinking,
      };
      return { ...state, stream };
    }
    if (ev.type === "thinking.delta") {
      const stream = {
        runId: ev.run_id,
        text: state.stream.text,
        thinking: state.stream.thinking + asString(data.text),
      };
      return { ...state, stream };
    }
    return state;
  }
  if (state.events.some((e) => e.seq === ev.seq)) return state;

  const events = state.events.slice();
  let at = events.length;
  while (at > 0 && events[at - 1].seq > ev.seq) at--;
  events.splice(at, 0, ev);

  let contiguousSeq = state.contiguousSeq;
  // 前 contiguousSeq 个就是 1..contiguousSeq，从它们后面接着数
  let i = contiguousSeq;
  while (i < events.length && events[i].seq === contiguousSeq + 1) {
    contiguousSeq++;
    i++;
  }
  const next = { ...state, events, lastSeq: Math.max(state.lastSeq, ev.seq), contiguousSeq };
  // 比已处理的更旧的事件（回放补来的）只进列表，不改衍生状态
  return ev.seq >= state.lastSeq ? foldDerived(next, ev) : next;
}

/** 回放漏掉了事件：收到的最大序号比连续收齐的大 */
export const hasGap = (state: AgentSessionState) => state.lastSeq > state.contiguousSeq;

/** 会话里还占着画布的那一轮运行（最新一个进行中的），没有为 null */
export function activeRunId(state: AgentSessionState): string | null {
  let found: string | null = null;
  for (const [id, run] of Object.entries(state.runs)) {
    if (ACTIVE_RUN_STATUSES.includes(run.status)) found = id;
  }
  return found;
}
