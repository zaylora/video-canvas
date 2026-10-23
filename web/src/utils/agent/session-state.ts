import type { AgentApprovalDto, AgentEventDto, AgentRunStatus } from "@/api/agent/type";

import { normalizeFeedback, type AgentFeedback } from "./feedback";

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
  /**
   * 连续区间的起点：events 里最小的序号减 1。整段会话都在手里时是 0；只加载了最近几页时是第一条的序号减 1，
   * 更早的事件还在服务端，往上翻时才取
   */
  baseSeq: number;
  /** 从 baseSeq 之后连续收齐到的最大序号：回放从它之后拉，中间漏掉的会补上 */
  contiguousSeq: number;
  /** 最近一页聊天记录已经加载过：为 false 时事件只来自实时推送，不能当作完整的结尾 */
  historyReady: boolean;
  /** 更早还有没加载的事件 */
  hasMoreOlder: boolean;
  /** 正在加载：最新一页（刚打开）或更早的一页（往上翻），没有为 null */
  loading: "latest" | "older" | null;
  /** 最近一次加载失败了：界面据此给「重试」，不自动反复请求 */
  loadError: boolean;
  /** 流式输出中的文字 */
  stream: AgentStream;
  /** 各轮运行的实时状态 */
  runs: Record<string, AgentRunLive>;
  /** 各个审批的最新状态（含提问） */
  approvals: Record<string, AgentApprovalDto>;
  /** 各轮运行最新的计划 */
  plans: Record<string, AgentPlanStep[]>;
  /** 各轮运行的用户反馈（没评价的轮没有这一项），由 feedback.set / feedback.cleared 事件和本地乐观更新维护 */
  feedback: Record<string, AgentFeedback>;
  /** 会话当前的上下文大小（Token）：最近一次对话调用送进模型的输入，压缩之后会变小；还没有调用过是 0 */
  contextTokens: number;
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
  baseSeq: 0,
  contiguousSeq: 0,
  historyReady: false,
  hasMoreOlder: false,
  loading: null,
  loadError: false,
  stream: { runId: null, text: "", thinking: "" },
  runs: {},
  approvals: {},
  plans: {},
  feedback: {},
  contextTokens: 0,
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
      // 上下文用量是会话级的：下一轮开始前界面还要显示上一轮的值
      const contextTokens =
        typeof data.context_tokens === "number" ? data.context_tokens : state.contextTokens;
      return {
        ...state,
        contextTokens,
        runs: { ...state.runs, [ev.run_id]: { ...prev, spent } },
      };
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
    case "feedback.set": {
      const runId = asString(data.run_id) || ev.run_id;
      const feedback = normalizeFeedback(data);
      if (!runId || !feedback) return state;
      return { ...state, feedback: { ...state.feedback, [runId]: feedback } };
    }
    case "feedback.cleared": {
      const runId = asString(data.run_id) || ev.run_id;
      if (!runId || !(runId in state.feedback)) return state;
      const { [runId]: _cleared, ...feedback } = state.feedback;
      return { ...state, feedback };
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
  // events 最前面 contiguousSeq - baseSeq 个就是 baseSeq+1..contiguousSeq，从它们后面接着数
  let i = contiguousSeq - state.baseSeq;
  while (i < events.length && events[i].seq === contiguousSeq + 1) {
    contiguousSeq++;
    i++;
  }
  const next = { ...state, events, lastSeq: Math.max(state.lastSeq, ev.seq), contiguousSeq };
  // 比已处理的更旧的事件（回放补来的）只进列表，不改衍生状态
  return ev.seq >= state.lastSeq ? foldDerived(next, ev) : next;
}

/**
 * 把一页聊天记录（事件升序）并进会话：第一页是最新的，之后每页更早；hasMore 说明再往前还有没有。
 * 事件按序号并入、去重；衍生状态（运行状态、审批、计划、评价、上下文用量）按全部已加载事件重新折一遍，
 * 所以旧页补进来后各个键仍以序号最大的事件为准（比如旧页里的评价已在新页里被取消，不会复活）。
 * 流式文字、已知的运行花费是实时才有的，保留。
 */
export function applyHistoryPage(
  state: AgentSessionState,
  page: AgentEventDto[],
  hasMore: boolean,
): AgentSessionState {
  const bySeq = new Map<number, AgentEventDto>();
  for (const e of state.events) bySeq.set(e.seq, e);
  for (const e of page) if (e.seq > 0) bySeq.set(e.seq, e);
  const events = [...bySeq.values()].sort((a, b) => a.seq - b.seq);

  const baseSeq = events.length > 0 ? events[0].seq - 1 : state.baseSeq;
  let contiguousSeq = baseSeq;
  for (const e of events) {
    if (e.seq !== contiguousSeq + 1) break;
    contiguousSeq++;
  }
  const lastSeq = Math.max(state.lastSeq, contiguousSeq, events.at(-1)?.seq ?? 0);

  let folded: AgentSessionState = { ...emptySession(), events, lastSeq };
  for (const e of events) folded = foldDerived(folded, e);
  const runs = { ...state.runs };
  for (const [id, run] of Object.entries(folded.runs)) {
    runs[id] = { ...run, spent: run.spent ?? state.runs[id]?.spent };
    if (runs[id].spent === undefined) delete runs[id].spent;
  }

  return {
    ...state,
    events,
    lastSeq,
    baseSeq,
    contiguousSeq,
    historyReady: true,
    hasMoreOlder: hasMore,
    loading: null,
    loadError: false,
    runs,
    approvals: folded.approvals,
    plans: folded.plans,
    feedback: folded.feedback,
    contextTokens: folded.contextTokens || state.contextTokens,
  };
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
