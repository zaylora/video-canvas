import { create } from "zustand";

import { getAgentEvents, listAgentSessions } from "@/api/agent";
import type { AgentEventDto, AgentRunDto, AgentSessionDto } from "@/api/agent/type";
import { applyAgentEvent, emptySession, type AgentSessionState } from "@/utils/agent/session-state";

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
  /** 收到一条事件（WebSocket 实时推送或回放） */
  handleEvent: (event: AgentEventDto) => void;
  /** 拉回会话里漏掉的事件：从连续收齐的序号之后分页拉到没有为止 */
  replay: (sessionId: string) => Promise<void>;
  /** 把 HTTP 返回的运行快照并进会话（发起、停止、继续之后，不必等事件） */
  ingestRun: (run: AgentRunDto) => void;
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

  handleEvent: (event) =>
    set((s) => {
      const current = s.sessions[event.session_id] ?? emptySession();
      const next = applyAgentEvent(current, event);
      return next === current ? s : { sessions: { ...s.sessions, [event.session_id]: next } };
    }),

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

  ingestRun: (run) =>
    set((s) => {
      const current = s.sessions[run.session_id] ?? emptySession();
      const live = current.runs[run.id];
      if (live && live.status === run.status) return s;
      const runs = { ...current.runs, [run.id]: { status: run.status, error: run.error_message } };
      return { sessions: { ...s.sessions, [run.session_id]: { ...current, runs } } };
    }),
}));
