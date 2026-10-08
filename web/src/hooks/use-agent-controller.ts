import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  cancelAgentRun,
  createAgentSession,
  decideAgentApproval,
  deleteAgentSession,
  getAgentModels,
  interjectAgentRun,
  renameAgentSession,
  resumeAgentRun,
  startAgentRun,
  undoAgentRun,
} from "@/api/agent";
import type {
  AgentMode,
  AgentModelDto,
  AgentSessionDto,
  DecideAgentApprovalReq,
} from "@/api/agent/type";
import { ApiError } from "@/utils/requests/request";
import { useAgentSettings } from "@/store/agent-settings";
import { useAgentStore } from "@/store/agent";
import { useCreditsStore } from "@/store/credits";
import { activeRunId, emptySession } from "@/utils/agent/session-state";
import { buildRunStrip, buildStatusLine, buildTimeline } from "@/utils/agent/timeline";

/** 后端错误码：超出本轮积分预算 */
const OVER_BUDGET_CODE = 60008;

/** 会话标题自动取首条消息的前几个字 */
const AUTO_TITLE_LEN = 14;

/** 没有会话时共用的空数组，免得每次渲染都是新引用 */
const NO_SESSIONS: AgentSessionDto[] = [];

/** 可用的 Agent 模型：打开浮窗时拉一次；没有已发布的模型时入口按钮变淡 */
export function useAgentModels() {
  const [models, setModels] = useState<AgentModelDto[] | null>(null);
  useEffect(() => {
    let active = true;
    void getAgentModels()
      .then((list) => active && setModels(list))
      .catch(() => active && setModels([]));
    return () => {
      active = false;
    };
  }, []);
  return models;
}

/**
 * 画布 Agent 浮窗的全部状态和动作：会话列表与当前会话、发送 / 停止 / 插话、审批决定、撤销本轮、继续。
 * 数据都来自 agent store（WebSocket 推送 + HTTP 回放）；这里只管「现在看哪个会话」和调用接口。
 * 接口失败的提示由全局请求拦截器弹出，这里只恢复状态。
 */
export function useAgentController({
  canvasId,
  models,
  getSelection,
  getViewport,
}: {
  canvasId: string;
  models: AgentModelDto[];
  /** 当前选中的节点 id */
  getSelection: () => string[];
  /** 当前视口 */
  getViewport: () => { x: number; y: number; zoom: number };
}) {
  const settings = useAgentSettings();
  const sessions = useAgentStore((s) => s.sessionsByCanvas[canvasId]) ?? NO_SESSIONS;
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [mode, setMode] = useState<AgentMode>("all");
  const [sending, setSending] = useState(false);
  /** 用户点 ✕ 隐藏了计划的那几轮运行 */
  const [hiddenPlans, setHiddenPlans] = useState<ReadonlySet<string>>(new Set());
  /** 已经撤销过的运行（撤销在服务端只能做一次） */
  const [undone, setUndone] = useState<ReadonlySet<string>>(new Set());
  const state =
    useAgentStore((s) => (sessionId ? s.sessions[sessionId] : undefined)) ?? emptySession();
  const session = sessions.find((s) => s.id === sessionId) ?? null;
  const sendingRef = useRef(false);

  const modelKey = models.some((m) => m.key === settings.modelKey)
    ? settings.modelKey
    : (models[0]?.key ?? "");

  /** 切到一个会话：先放上去，再把事件从头补齐（已经收齐的部分不重复拉） */
  const openSession = useCallback((target: AgentSessionDto | null) => {
    setSessionId(target?.id ?? null);
    if (target) {
      setMode(target.mode);
      void useAgentStore
        .getState()
        .replay(target.id)
        .catch(() => undefined);
    } else {
      setMode("all");
    }
  }, []);

  // 打开画布：拉会话列表，默认接着最近的一个
  useEffect(() => {
    let active = true;
    void useAgentStore
      .getState()
      .loadSessions(canvasId)
      .then((list) => active && openSession(list[0] ?? null))
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [canvasId, openSession]);

  const activeRun = useMemo(() => activeRunId(state), [state]);
  const timeline = useMemo(() => buildTimeline(state), [state]);
  const strip = useMemo(() => buildRunStrip(state, hiddenPlans), [state, hiddenPlans]);
  const statusLine = useMemo(() => buildStatusLine(state), [state]);
  /** 进行中那一轮在等你决定的审批或提问（运行到审批就暂停，同一时间最多一个）：决定框钉在输入框的位置 */
  const pending = useMemo(() => {
    if (!activeRun) return null;
    const list = Object.values(state.approvals).filter(
      (a) => a.run_id === activeRun && a.status === "pending",
    );
    return list.at(-1) ?? null;
  }, [activeRun, state.approvals]);
  /** 本轮用量：运行中取运行的已花和预算，空闲时已花为 0、预算取设置 */
  const live = activeRun ? state.runs[activeRun] : undefined;
  const usage = {
    spent: live?.spent ?? 0,
    budget: live?.budget ?? settings.budget,
  };

  /** 确保有会话可发：没有就按当前模式和模型新建一个 */
  const ensureSession = useCallback(async () => {
    if (session) return session;
    const created = await createAgentSession(canvasId, { mode, model_key: modelKey || undefined });
    useAgentStore.getState().upsertSession(created);
    setSessionId(created.id);
    return created;
  }, [canvasId, mode, modelKey, session]);

  /** 发送：运行中就是插话，否则发起新一轮。返回是否发出去了（没发出去输入框要保留原文） */
  const send = useCallback(
    async (text: string, withSelection = true) => {
      const message = text.trim();
      if (!message || sendingRef.current) return false;
      sendingRef.current = true;
      setSending(true);
      try {
        if (activeRun) {
          await interjectAgentRun(activeRun, message);
          return true;
        }
        const target = await ensureSession();
        const run = await startAgentRun(target.id, {
          message,
          mode,
          selection: withSelection ? getSelection() : [],
          viewport: getViewport(),
          budget_credits: settings.budget,
          agent_model_key: modelKey || undefined,
        });
        useAgentStore.getState().ingestRun(run);
        // 首条消息后标题自动取前几个字
        if (target.title === "新对话" && target.last_seq === 0) {
          const title = [...message].slice(0, AUTO_TITLE_LEN).join("");
          void renameAgentSession(target.id, title)
            .then(() => useAgentStore.getState().upsertSession({ ...target, title }))
            .catch(() => undefined);
        }
        void useCreditsStore.getState().refresh();
        return true;
      } catch {
        return false;
      } finally {
        sendingRef.current = false;
        setSending(false);
      }
    },
    [activeRun, ensureSession, getSelection, getViewport, mode, modelKey, settings.budget],
  );

  const stop = useCallback(async () => {
    if (!activeRun) return;
    try {
      useAgentStore.getState().ingestRun(await cancelAgentRun(activeRun));
    } catch {
      // 全局提示已经弹了
    }
  }, [activeRun]);

  const resume = useCallback(async (runId: string, addBudget = 0) => {
    try {
      useAgentStore.getState().ingestRun(await resumeAgentRun(runId, addBudget));
    } catch {
      // 全局提示已经弹了
    }
  }, []);

  /** 对审批做决定；超出本轮预算单独报出来，卡片据此提供「追加预算并批准」 */
  const decide = useCallback(
    async (
      approvalId: string,
      req: DecideAgentApprovalReq,
    ): Promise<"ok" | "over_budget" | "failed"> => {
      try {
        await decideAgentApproval(approvalId, req);
        void useCreditsStore.getState().refresh();
        return "ok";
      } catch (error) {
        return error instanceof ApiError && error.code === OVER_BUDGET_CODE
          ? "over_budget"
          : "failed";
      }
    },
    [],
  );

  /** 撤销本轮；返回结果（成功）或 null */
  const undo = useCallback(async (runId: string) => {
    try {
      const result = await undoAgentRun(runId);
      setUndone((prev) => new Set(prev).add(runId));
      return result;
    } catch {
      return null;
    }
  }, []);

  const newSession = useCallback(() => openSession(null), [openSession]);

  const rename = useCallback(
    async (title: string) => {
      if (!session) return;
      const next = title.trim() || session.title;
      try {
        await renameAgentSession(session.id, next);
        useAgentStore.getState().upsertSession({ ...session, title: next });
      } catch {
        // 全局提示已经弹了
      }
    },
    [session],
  );

  const remove = useCallback(
    async (target: AgentSessionDto) => {
      try {
        await deleteAgentSession(target.id);
      } catch {
        return;
      }
      useAgentStore.getState().removeSession(canvasId, target.id);
      if (target.id === sessionId) {
        const rest = (useAgentStore.getState().sessionsByCanvas[canvasId] ?? []).filter(
          (s) => s.id !== target.id,
        );
        openSession(rest[0] ?? null);
      }
    },
    [canvasId, openSession, sessionId],
  );

  const hidePlan = useCallback(
    (runId: string) => setHiddenPlans((prev) => new Set(prev).add(runId)),
    [],
  );

  return {
    sessions,
    session,
    state,
    timeline,
    strip,
    statusLine,
    pending,
    usage,
    activeRun,
    busy: activeRun !== null,
    sending,
    mode,
    setMode,
    modelKey,
    undone,
    send,
    stop,
    resume,
    decide,
    undo,
    openSession,
    newSession,
    rename,
    remove,
    hidePlan,
  };
}

export type AgentController = ReturnType<typeof useAgentController>;
