import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, test } from "bun:test";
import type {
  act as ReactAct,
  createElement as CreateElement,
  useEffect as UseEffect,
} from "react";
import type { Root, createRoot as CreateRoot } from "react-dom/client";

import { mockAgentApi } from "@/tests/helpers/agent-api";

import type {
  AdminPageContext,
  AgentApprovalDto,
  AgentMode,
  AgentModelDto,
  AgentSessionDto,
  StartAgentRunReq,
} from "@/api/agent/type";
import type {
  AgentController,
  useAgentController as UseAgentController,
} from "@/hooks/use-agent-controller";
import type { useAgentStore as UseAgentStore } from "@/store/agent";
import type { useCreditsStore as UseCreditsStore } from "@/store/credits";

/**
 * useAgentController 的后台助手场景（mode: "admin"）：会话列表 / 新建走后台助手接口，
 * 发起运行时带 page_context、不带视口；画布和短剧助手的调用保持不变。
 * 在 jsdom 里挂一个只调用该 hook 的组件，用假的 @/api/agent 记录调用
 */

const GLOBALS = [
  "window",
  "document",
  "navigator",
  "localStorage",
  "Node",
  "Element",
  "HTMLElement",
  "Event",
  "CustomEvent",
  "MutationObserver",
  "getComputedStyle",
  "requestAnimationFrame",
  "cancelAnimationFrame",
  "Notification",
];
const saved: Record<string, unknown> = {};
let win: Window & typeof globalThis;
let act: typeof ReactAct;
let createRoot: typeof CreateRoot;
let createElement: typeof CreateElement;
let useEffect: typeof UseEffect;
let useAgentController: typeof UseAgentController;
let useAgentStore: typeof UseAgentStore;
let useCreditsStore: typeof UseCreditsStore;

const models: AgentModelDto[] = [
  { key: "m1", name: "助手模型", vision: true, context_window: 1000 } as AgentModelDto,
];

const sessionOf = (id: string, canvasId: string, mode: AgentMode): AgentSessionDto => ({
  id,
  canvas_id: canvasId,
  title: "新对话",
  mode,
  model_key: "m1",
  last_seq: 0,
  created_at: "2026-11-02T06:00:00Z",
  updated_at: "2026-11-02T06:00:00Z",
});

const decided: AgentApprovalDto = {
  id: "ap1",
  run_id: "r1",
  kind: "admin_change",
  status: "executed",
  payload: {} as never,
  quote_credits: 0,
  decision: null,
  result: null,
  expires_at: "",
};

/** 记下的接口调用：函数名 + 参数 */
type Call = { name: string; args: unknown[] };
let calls: Call[];
/** 后台助手会话列表接口返回的会话 */
let adminList: AgentSessionDto[];
/** 为 true 时决定审批的接口报错 */
let failDecide = false;

const names = (name: string) => calls.filter((c) => c.name === name);
const rec =
  <T,>(name: string, result: (...args: unknown[]) => T) =>
  async (...args: unknown[]) => {
    calls.push({ name, args });
    return result(...args);
  };

/**
 * API 替身按用例安装并恢复，保留真实模块的导出集合，避免依赖测试文件的加载顺序。
 */
const fakeApi = () => ({
  getAgentModels: rec("getAgentModels", () => models),
  listAgentSessions: rec("listAgentSessions", () => []),
  createAgentSession: rec("createAgentSession", (_c, data) =>
    sessionOf("cs1", "c1", (data as { mode: AgentMode }).mode),
  ),
  listAdminAssistantSessions: rec("listAdminAssistantSessions", () => adminList),
  createAdminAssistantSession: rec("createAdminAssistantSession", () =>
    sessionOf("as1", "0", "admin"),
  ),
  renameAgentSession: rec("renameAgentSession", () => undefined),
  deleteAgentSession: rec("deleteAgentSession", () => undefined),
  getAgentEvents: rec("getAgentEvents", () => []),
  getAgentHistory: rec("getAgentHistory", () => ({ events: [], has_more: false })),
  startAgentRun: rec("startAgentRun", () => ({ id: "r1", session_id: "x", status: "running" })),
  interjectAgentRun: rec("interjectAgentRun", () => undefined),
  cancelAgentRun: rec("cancelAgentRun", () => ({ id: "r1", session_id: "x", status: "canceled" })),
  resumeAgentRun: rec("resumeAgentRun", () => ({ id: "r1", session_id: "x", status: "running" })),
  undoAgentRun: rec("undoAgentRun", () => ({})),
  decideAgentApproval: rec("decideAgentApproval", () => {
    if (failDecide) throw new Error("网络错误");
    return decided;
  }),
  setAgentFeedback: rec("setAgentFeedback", () => ({ rating: 1, reasons: [], comment: "" })),
  deleteAgentFeedback: rec("deleteAgentFeedback", () => undefined),
  getAgentWatches: rec("getAgentWatches", () => []),
  cancelAgentWatches: rec("cancelAgentWatches", () => []),
});

beforeAll(async () => {
  const { JSDOM } = await import("jsdom");
  const jsdom = new JSDOM("<!doctype html><html><body></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  win = jsdom.window as unknown as Window & typeof globalThis;
  const g = globalThis as unknown as Record<string, unknown>;
  for (const key of GLOBALS) {
    saved[key] = g[key];
    Object.defineProperty(g, key, {
      value: (jsdom.window as unknown as Record<string, unknown>)[key],
      configurable: true,
      writable: true,
    });
  }
  g.IS_REACT_ACT_ENVIRONMENT = true;
  const react = await import("react");
  act = react.act;
  createElement = react.createElement;
  useEffect = react.useEffect;
  createRoot = (await import("react-dom/client")).createRoot;
  useAgentController = (await import("@/hooks/use-agent-controller")).useAgentController;
  useAgentStore = (await import("@/store/agent")).useAgentStore;
  useCreditsStore = (await import("@/store/credits")).useCreditsStore;
  realRefresh = useCreditsStore.getState().refresh;
});

afterAll(() => {
  const g = globalThis as unknown as Record<string, unknown>;
  for (const key of GLOBALS) {
    Object.defineProperty(g, key, { value: saved[key], configurable: true, writable: true });
  }
  delete g.IS_REACT_ACT_ENVIRONMENT;
});

/** 积分 store 原来的刷新函数 */
let realRefresh: () => Promise<void>;
/** 还没卸载的组件：断言失败时也要卸载，否则它订阅着 store，会在后面的测试文件里报错 */
const mounted: Array<() => Promise<void>> = [];

/** 卸载组件后恢复当前用例的 API 替身。 */
let restoreApi: () => void;

beforeEach(() => {
  restoreApi = mockAgentApi(fakeApi());
  calls = [];
  adminList = [];
  failDecide = false;
  useAgentStore.setState({ sessions: {}, sessionsByCanvas: {} });
  // 画布发送后会刷新积分，不让它真的发请求（afterEach 还原）
  useCreditsStore.setState({ refresh: async () => {} });
});
afterEach(async () => {
  useCreditsStore.setState({ refresh: realRefresh });
  for (const unmount of mounted.splice(0)) {
    try {
      await unmount();
    } catch {
      // 用例里已经卸载过了
    }
  }
  restoreApi();
});

type Args = Parameters<typeof UseAgentController>[0];

async function mount(args: Args) {
  const out: { ctl: AgentController | null } = { ctl: null };
  function Probe() {
    const ctl = useAgentController(args);
    useEffect(() => {
      out.ctl = ctl;
    });
    return null;
  }
  const container = win.document.createElement("div");
  let root: Root;
  await act(async () => {
    root = createRoot(container);
    root.render(createElement(Probe));
  });
  // 等会话列表请求完成
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
  const unmount = () => act(async () => root.unmount());
  mounted.push(unmount);
  return { out, unmount };
}

const send = (out: { ctl: AgentController | null }, text: string) =>
  act(async () => {
    await out.ctl!.send(text);
  });

/** 发起运行时的请求体 */
const runBody = () => names("startAgentRun")[0]?.args[1] as StartAgentRunReq;

const context: AdminPageContext = { route: "/admin/ai/channels", model_key: "kling-v2-1" };

describe("useAgentController：后台助手场景", () => {
  test("打开时拉后台助手会话列表，不碰画布的会话接口", async () => {
    const m = await mount({ canvasId: "0", models, mode: "admin" });
    expect(names("listAdminAssistantSessions")).toHaveLength(1);
    expect(names("listAgentSessions")).toHaveLength(0);
    await m.unmount();
  });

  test("接着最近的一个后台会话，会话存在 ADMIN 作用域里，不拉后台等待", async () => {
    adminList = [sessionOf("as9", "0", "admin")];
    const m = await mount({ canvasId: "0", models, mode: "admin" });
    expect(m.out.ctl?.session?.id).toBe("as9");
    expect(useAgentStore.getState().sessionsByCanvas["0"]?.map((s) => s.id)).toEqual(["as9"]);
    expect(names("getAgentWatches")).toHaveLength(0);
    await m.unmount();
  });

  test("发送：没有会话时新建后台会话，运行带 mode=admin 和 page_context，不带视口", async () => {
    const m = await mount({ canvasId: "0", models, mode: "admin", pageContext: context });
    await send(m.out, "把可灵涨价 10%");
    expect(names("createAdminAssistantSession")[0].args).toEqual([{ model_key: "m1" }]);
    expect(names("createAgentSession")).toHaveLength(0);
    expect(names("startAgentRun")[0].args[0]).toBe("as1");
    const body = runBody();
    expect(body.mode).toBe("admin");
    expect(body.message).toBe("把可灵涨价 10%");
    expect(body.page_context).toEqual(context);
    expect(body.agent_model_key).toBe("m1");
    expect(body.viewport).toBeUndefined();
    await m.unmount();
  });

  test("没有页面上下文时不带 page_context", async () => {
    const m = await mount({ canvasId: "0", models, mode: "admin" });
    await send(m.out, "你好");
    expect("page_context" in runBody()).toBe(false);
    await m.unmount();
  });

  test("本机发送时记下的页面上下文，补到事件里没带 page_context 的用户消息上", async () => {
    const m = await mount({ canvasId: "0", models, mode: "admin", pageContext: context });
    await send(m.out, "改价");
    await act(async () => {
      useAgentStore.getState().handleEvent(
        {
          session_id: "as1",
          canvas_id: "0",
          run_id: "r1",
          seq: 1,
          type: "message.user",
          data: { text: "改价" },
          created_at: "2026-11-02T06:00:00Z",
        },
        {},
      );
    });
    const user = m.out.ctl?.timeline.find((i) => i.type === "user");
    expect(user && "pageContext" in user ? user.pageContext : undefined).toEqual(context);
    await m.unmount();
  });

  test("对变更单做决定：返回的审批立刻并进会话", async () => {
    adminList = [sessionOf("as1", "0", "admin")];
    const m = await mount({ canvasId: "0", models, mode: "admin" });
    let result = "";
    await act(async () => {
      result = await m.out.ctl!.decide("ap1", { decision: "approve" });
    });
    expect(result).toBe("ok");
    expect(useAgentStore.getState().sessions["as1"]?.approvals["ap1"]?.status).toBe("executed");
    await m.unmount();
  });
});

describe("useAgentController：画布和短剧的调用不变", () => {
  test("画布：走画布的会话接口，运行带视口、mode=all，不带 page_context", async () => {
    const viewport = { x: 1, y: 2, zoom: 1.5 };
    const m = await mount({
      canvasId: "c1",
      models,
      getViewport: () => viewport,
      // 后台才认这个参数
      pageContext: context,
    });
    expect(names("listAgentSessions")[0].args).toEqual(["c1"]);
    expect(names("listAdminAssistantSessions")).toHaveLength(0);
    await send(m.out, "搭个分镜");
    expect(names("createAgentSession")[0].args).toEqual(["c1", { mode: "all", model_key: "m1" }]);
    expect(names("createAdminAssistantSession")).toHaveLength(0);
    const body = runBody();
    expect(body.mode).toBe("all");
    expect(body.viewport).toEqual(viewport);
    expect("page_context" in body).toBe(false);
    await m.unmount();
  });

  test("短剧：mode=drama，会话建在短剧画布上，不带视口和 page_context", async () => {
    const m = await mount({ canvasId: "c1", models, mode: "drama" });
    await send(m.out, "改剧本");
    expect(names("createAgentSession")[0].args).toEqual(["c1", { mode: "drama", model_key: "m1" }]);
    const body = runBody();
    expect(body.mode).toBe("drama");
    expect(body.viewport).toBeUndefined();
    expect("page_context" in body).toBe(false);
    await m.unmount();
  });

  test("mode=admin 但不是后台作用域（canvasId 不是 0）：仍按画布处理，不误走后台接口", async () => {
    const m = await mount({ canvasId: "c1", models, mode: "admin" });
    expect(names("listAgentSessions")[0].args).toEqual(["c1"]);
    expect(names("listAdminAssistantSessions")).toHaveLength(0);
    await m.unmount();
  });
});

describe("useAgentController：等你回答时在输入框打字", () => {
  const waitingAsk = (): AgentApprovalDto => ({
    id: "ask1",
    run_id: "r1",
    kind: "ask",
    status: "pending",
    payload: { question: "画幅？", kind: "choice", options: ["横屏", "竖屏"] },
    quote_credits: 0,
    decision: null,
    result: null,
    expires_at: "",
  });

  /** 会话里有一轮停在提问上的运行 */
  async function mountWaiting() {
    adminList = [sessionOf("as1", "0", "admin")];
    const m = await mount({ canvasId: "0", models, mode: "admin" });
    await act(async () => {
      useAgentStore.getState().ingestRun({
        id: "r1",
        session_id: "as1",
        status: "waiting_input",
        context_tokens: 0,
        spent_credits: 0,
      } as never);
      useAgentStore.getState().ingestApproval("as1", waitingAsk());
    });
    expect(m.out.ctl?.pending?.id).toBe("ask1");
    return m;
  }

  test("发消息算新消息：先拒绝（不回答）这条提问，再把消息作为插话交给运行", async () => {
    const m = await mountWaiting();
    let sent = false;
    await act(async () => {
      sent = await m.out.ctl!.send("改成竖屏吧");
    });
    expect(sent).toBe(true);
    expect(names("decideAgentApproval")[0].args).toEqual(["ask1", { decision: "reject" }]);
    expect(names("interjectAgentRun")[0].args.slice(0, 2)).toEqual(["r1", "改成竖屏吧"]);
    expect(names("startAgentRun")).toHaveLength(0);
    // 先拒绝、后插话：顺序不能反
    expect(
      calls
        .map((c) => c.name)
        .filter((n) => n === "decideAgentApproval" || n === "interjectAgentRun"),
    ).toEqual(["decideAgentApproval", "interjectAgentRun"]);
    await m.unmount();
  });

  test("非提问的审批待确认时发消息：不调用 decide，直接插话，审批保持待确认", async () => {
    const m = await mountWaiting();
    await act(async () => {
      useAgentStore.getState().ingestRun({
        id: "r1",
        session_id: "as1",
        status: "waiting_approval",
        context_tokens: 0,
        spent_credits: 0,
      } as never);
      useAgentStore.getState().ingestApproval("as1", {
        ...waitingAsk(),
        id: "gen1",
        kind: "generate",
        payload: {} as never,
      });
    });
    // 提问那条换成已处理，剩下待确认的只有生成审批
    await act(async () => {
      useAgentStore.getState().ingestApproval("as1", { ...waitingAsk(), status: "rejected" });
    });
    expect(m.out.ctl?.pending?.id).toBe("gen1");
    let sent = false;
    await act(async () => {
      sent = await m.out.ctl!.send("背景换成夜景");
    });
    expect(sent).toBe(true);
    expect(names("decideAgentApproval")).toHaveLength(0);
    expect(names("interjectAgentRun")[0].args.slice(0, 2)).toEqual(["r1", "背景换成夜景"]);
    expect(m.out.ctl?.pending?.id).toBe("gen1");
    await m.unmount();
  });

  test("拒绝没成功就不发消息，输入框保留原文", async () => {
    const m = await mountWaiting();
    failDecide = true;
    let sent = true;
    await act(async () => {
      sent = await m.out.ctl!.send("改成竖屏吧");
    });
    expect(sent).toBe(false);
    expect(names("interjectAgentRun")).toHaveLength(0);
    await m.unmount();
  });
});
