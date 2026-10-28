import { create } from "zustand";

import { toast } from "sonner";

import {
  cancelAgentWatches,
  deleteAgentFeedback,
  getAgentEvents,
  getAgentHistory,
  getAgentWatches,
  listAgentSessions,
  setAgentFeedback,
} from "@/api/agent";
import type {
  AgentEventDto,
  AgentRunDto,
  AgentSessionDto,
  AgentWatchBatchDto,
  SetAgentFeedbackReq,
} from "@/api/agent/type";
import {
  FEEDBACK_SAVE_FAILED,
  normalizeFeedback,
  type AgentFeedback,
} from "@/utils/agent/feedback";
import {
  applyAgentEvent,
  applyHistoryPage,
  emptySession,
  type AgentSessionState,
} from "@/utils/agent/session-state";

/** 实时事件的监听者：只收 WebSocket 实时推来的、第一次到达的持久化事件（回放、重复到达的不收），给系统通知用 */
type LiveListener = (event: AgentEventDto) => void;
const liveListeners = new Set<LiveListener>();

/**
 * 订阅实时到达的 Agent 事件；回放补来的历史事件不会触发，避免对已经发生过的事重复提醒
 * @param listener 收到事件的回调
 * @returns 取消订阅
 */
export function subscribeLiveAgentEvents(listener: LiveListener): () => void {
  liveListeners.add(listener);
  return () => {
    liveListeners.delete(listener);
  };
}

/** 一页回放最多拉多少页，防止服务端异常时死循环 */
const MAX_REPLAY_PAGES = 50;

type AgentStore = {
  /** 各画布的会话列表（最近更新的在前） */
  sessionsByCanvas: Record<string, AgentSessionDto[]>;
  /** 各会话的前端状态 */
  sessions: Record<string, AgentSessionState>;
  /** 加载画布上的会话列表 */
  loadSessions: (canvasId: string) => Promise<AgentSessionDto[]>;
  /** 把会话放进列表（新建、改名后） */
  upsertSession: (session: AgentSessionDto) => void;
  /** 从列表里去掉会话（删除后） */
  removeSession: (canvasId: string, sessionId: string) => void;
  /** 收到一条事件；live 表示是 WebSocket 实时推来的（回放不带），实时的新事件会通知 subscribeLiveAgentEvents 的监听者 */
  handleEvent: (event: AgentEventDto, opts?: { live?: boolean }) => void;
  /** 读会话里进行中的后台等待并进状态（打开会话时）；事件里已有的批以事件为准；失败不抛出，条就不显示 */
  loadWatches: (sessionId: string) => Promise<void>;
  /** 不用等了：取消还没交给 Agent 的等待，被取消的批标成 canceled；生成照常完成 */
  cancelWatches: (sessionId: string) => Promise<void>;
  /** 拉回会话里漏掉的事件：从连续收齐的序号之后分页拉到没有为止 */
  replay: (sessionId: string) => Promise<void>;
  /** 打开会话：取最新一页聊天记录（加载过就什么也不做）；失败不抛出，留下 loadError 让界面给重试 */
  loadLatest: (sessionId: string) => Promise<void>;
  /** 往上翻：取比已有事件更早的一页；没有更早的、正在加载、还没取过最新一页时什么也不做；失败同 loadLatest */
  loadOlder: (sessionId: string) => Promise<void>;
  /** 断线重连后对账：加载过聊天记录的补尾巴，没加载过的取最新一页（不从头拉整段会话） */
  sync: (sessionId: string) => Promise<void>;
  /** 把 HTTP 返回的运行快照并进会话（发起、停止、继续之后，不必等事件） */
  ingestRun: (run: AgentRunDto) => void;
  /**
   * 给一轮已结束的运行打分（再次提交即覆盖）：先乐观更新，保存失败回到提交前的评价并提示。
   * @returns 是否保存成功
   */
  submitFeedback: (sessionId: string, runId: string, req: SetAgentFeedbackReq) => Promise<boolean>;
  /**
   * 取消一轮运行的评价：先乐观清掉，失败则恢复原评价并提示。
   * @returns 是否取消成功
   */
  cancelFeedback: (sessionId: string, runId: string) => Promise<boolean>;
};

export const useAgentStore = create<AgentStore>((set, get) => ({
  sessionsByCanvas: {},
  sessions: {},

  loadSessions: async (canvasId) => {
    const list = await listAgentSessions(canvasId);
    set((s) => ({ sessionsByCanvas: { ...s.sessionsByCanvas, [canvasId]: list } }));
    return list;
  },

  upsertSession: (session) =>
    set((s) => {
      const list = s.sessionsByCanvas[session.canvas_id] ?? [];
      const rest = list.filter((x) => x.id !== session.id);
      return {
        sessionsByCanvas: { ...s.sessionsByCanvas, [session.canvas_id]: [session, ...rest] },
      };
    }),

  removeSession: (canvasId, sessionId) =>
    set((s) => {
      const { [sessionId]: _removed, ...sessions } = s.sessions;
      return {
        sessions,
        sessionsByCanvas: {
          ...s.sessionsByCanvas,
          [canvasId]: (s.sessionsByCanvas[canvasId] ?? []).filter((x) => x.id !== sessionId),
        },
      };
    }),

  handleEvent: (event, opts) => {
    // 会话标题变了（开始运行时的草稿、首轮结束后模型命名的结果）：改会话列表里的标题，不属于会话的事件流
    if (event.type === "session.titled") {
      const title = typeof event.data?.title === "string" ? event.data.title.trim() : "";
      if (title)
        set((s) => {
          const list = s.sessionsByCanvas[event.canvas_id];
          if (!list?.some((x) => x.id === event.session_id)) return s;
          return {
            sessionsByCanvas: {
              ...s.sessionsByCanvas,
              [event.canvas_id]: list.map((x) => (x.id === event.session_id ? { ...x, title } : x)),
            },
          };
        });
      return;
    }
    const known = get().sessions[event.session_id]?.events.some((e) => e.seq === event.seq);
    set((s) => {
      const current = s.sessions[event.session_id] ?? emptySession();
      const next = applyAgentEvent(current, event);
      return next === current ? s : { sessions: { ...s.sessions, [event.session_id]: next } };
    });
    if (opts?.live && event.seq > 0 && !known) for (const fn of [...liveListeners]) fn(event);
  },

  loadWatches: async (sessionId) => {
    let items: AgentWatchBatchDto[];
    try {
      items = await getAgentWatches(sessionId);
    } catch {
      return;
    }
    patchSession(sessionId, (x) => {
      const fresh = items.filter((b) => !(b.batch_id in x.watches));
      if (fresh.length === 0) return x;
      return {
        ...x,
        watches: { ...x.watches, ...Object.fromEntries(fresh.map((b) => [b.batch_id, b])) },
      };
    });
  },

  cancelWatches: async (sessionId) => {
    const items = await cancelAgentWatches(sessionId);
    patchSession(sessionId, (x) => ({
      ...x,
      watches: { ...x.watches, ...Object.fromEntries(items.map((b) => [b.batch_id, b])) },
    }));
  },

  replay: async (sessionId) => {
    for (let page = 0; page < MAX_REPLAY_PAGES; page++) {
      const after = (get().sessions[sessionId] ?? emptySession()).contiguousSeq;
      const events = await getAgentEvents(sessionId, after);
      if (events.length === 0) return;
      for (const event of events) get().handleEvent(event);
      // 没有推进说明服务端给的都是已有的，别再拉了
      if ((get().sessions[sessionId] ?? emptySession()).contiguousSeq <= after) return;
    }
  },

  loadLatest: async (sessionId) => {
    const current = get().sessions[sessionId] ?? emptySession();
    if (current.historyReady || current.loading) return;
    patchSession(sessionId, (x) => ({ ...x, loading: "latest", loadError: false }));
    try {
      const page = await getAgentHistory(sessionId, 0);
      patchSession(sessionId, (x) => applyHistoryPage(x, page.events, page.has_more));
    } catch {
      patchSession(sessionId, (x) => ({ ...x, loading: null, loadError: true }));
    }
  },

  loadOlder: async (sessionId) => {
    const current = get().sessions[sessionId] ?? emptySession();
    const before = current.events[0]?.seq;
    if (!current.historyReady || !current.hasMoreOlder || current.loading || !before) return;
    patchSession(sessionId, (x) => ({ ...x, loading: "older", loadError: false }));
    try {
      const page = await getAgentHistory(sessionId, before);
      patchSession(sessionId, (x) => applyHistoryPage(x, page.events, page.has_more));
    } catch {
      patchSession(sessionId, (x) => ({ ...x, loading: null, loadError: true }));
    }
  },

  sync: async (sessionId) => {
    if (get().sessions[sessionId]?.historyReady) await get().replay(sessionId);
    else await get().loadLatest(sessionId);
  },

  ingestRun: (run) =>
    set((s) => {
      const current = s.sessions[run.session_id] ?? emptySession();
      const live = current.runs[run.id];
      // 新一轮刚发起时它的上下文用量是 0，不能把会话已有的用量冲掉
      const contextTokens = run.context_tokens > 0 ? run.context_tokens : current.contextTokens;
      if (
        live &&
        live.status === run.status &&
        live.spent === run.spent_credits &&
        current.contextTokens === contextTokens
      )
        return s;
      const runs = {
        ...current.runs,
        [run.id]: { status: run.status, error: run.error_message, spent: run.spent_credits },
      };
      return {
        sessions: { ...s.sessions, [run.session_id]: { ...current, runs, contextTokens } },
      };
    }),

  submitFeedback: async (sessionId, runId, req) => {
    const before = feedbackOf(sessionId, runId);
    writeFeedback(sessionId, runId, {
      rating: req.rating,
      reasons: req.reasons ?? [],
      comment: req.comment ?? "",
    });
    try {
      writeFeedback(sessionId, runId, normalizeFeedback(await setAgentFeedback(runId, req)));
      return true;
    } catch {
      writeFeedback(sessionId, runId, before);
      toast.error(FEEDBACK_SAVE_FAILED);
      return false;
    }
  },

  cancelFeedback: async (sessionId, runId) => {
    const before = feedbackOf(sessionId, runId);
    writeFeedback(sessionId, runId, null);
    try {
      await deleteAgentFeedback(runId);
      return true;
    } catch {
      writeFeedback(sessionId, runId, before);
      toast.error(FEEDBACK_SAVE_FAILED);
      return false;
    }
  },
}));

/** 改某个会话的前端状态（没有就从空的开始） */
function patchSession(sessionId: string, update: (state: AgentSessionState) => AgentSessionState) {
  useAgentStore.setState((s) => ({
    sessions: { ...s.sessions, [sessionId]: update(s.sessions[sessionId] ?? emptySession()) },
  }));
}

/** 某轮运行当前的反馈，没有为 null */
const feedbackOf = (sessionId: string, runId: string): AgentFeedback | null =>
  useAgentStore.getState().sessions[sessionId]?.feedback[runId] ?? null;

/** 直接写某轮运行的反馈（乐观更新和失败回滚用）；null 表示没有评价 */
function writeFeedback(sessionId: string, runId: string, value: AgentFeedback | null) {
  useAgentStore.setState((s) => {
    const current = s.sessions[sessionId] ?? emptySession();
    const { [runId]: _old, ...rest } = current.feedback;
    const feedback = value ? { ...rest, [runId]: value } : rest;
    return { sessions: { ...s.sessions, [sessionId]: { ...current, feedback } } };
  });
}
