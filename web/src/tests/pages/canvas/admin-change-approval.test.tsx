import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  spyOn,
  test,
} from "bun:test";
import type { act as ReactAct } from "react";
import type { Root, createRoot as CreateRoot } from "react-dom/client";

import type {
  AdminChangeItem,
  AdminTestRunPayload,
  AgentApprovalDto,
  DecideAgentApprovalReq,
} from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import type { ApprovalRow as ApprovalRowType } from "@/pages/canvas/agent/approval-row";
import type { ApprovalCard as ApprovalCardType } from "@/pages/canvas/agent/approval-card";
import type { useAdminStore as UseAdminStore } from "@/store/admin";
import { useAgentStore } from "@/store/agent";
import service from "@/utils/requests/service";

/**
 * 后台助手的变更单（审批种类 admin_change）接入画布的决定框和记录行：
 * 用 jsdom 挂载真实组件、派发点击，检查提交给后端的请求体和记录行上的结果
 */

const item = (key: string, over: Partial<AdminChangeItem> = {}): AdminChangeItem => ({
  target: "model",
  key,
  label: key,
  live: true,
  status: "ok",
  issues: [],
  warnings: [],
  required_role: "admin",
  diff: [{ path: "pricing.per_second", before: 8, after: 10 }],
  basis: [],
  ...over,
});

const approval = (
  items: AdminChangeItem[],
  over: Partial<AgentApprovalDto> = {},
): AgentApprovalDto => ({
  id: "ap1",
  run_id: "r1",
  kind: "admin_change",
  status: "pending",
  payload: { order_no: "CO-1", title: "调价", reason: "按成本加价", items } as never,
  quote_credits: 0,
  decision: null,
  result: null,
  expires_at: "",
  ...over,
});

const GLOBALS = [
  "window",
  "document",
  "navigator",
  "localStorage",
  "Node",
  "Element",
  "HTMLElement",
  "HTMLInputElement",
  "HTMLTextAreaElement",
  "HTMLButtonElement",
  "HTMLFormElement",
  "HTMLSelectElement",
  "HTMLIFrameElement",
  "DocumentFragment",
  "MutationObserver",
  "Event",
  "MouseEvent",
  "KeyboardEvent",
  "FocusEvent",
  "InputEvent",
  "CustomEvent",
  "getComputedStyle",
  "requestAnimationFrame",
  "cancelAnimationFrame",
];
const saved: Record<string, unknown> = {};
let win: Window & typeof globalThis;
let act: typeof ReactAct;
let createRoot: typeof CreateRoot;
let ApprovalCard: typeof ApprovalCardType;
let ApprovalRow: typeof ApprovalRowType;
let useAdminStore: typeof UseAdminStore;

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
  act = (await import("react")).act;
  createRoot = (await import("react-dom/client")).createRoot;
  ApprovalCard = (await import("@/pages/canvas/agent/approval-card")).ApprovalCard;
  ApprovalRow = (await import("@/pages/canvas/agent/approval-row")).ApprovalRow;
  useAdminStore = (await import("@/store/admin")).useAdminStore;
});

afterAll(() => {
  const g = globalThis as unknown as Record<string, unknown>;
  for (const key of GLOBALS) {
    Object.defineProperty(g, key, { value: saved[key], configurable: true, writable: true });
  }
  delete g.IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => useAdminStore.setState({ role: "admin" }));
afterEach(() => {
  secretRequest?.mockRestore();
  secretRequest = undefined;
});

let secretRequest: ReturnType<typeof spyOn<typeof service, "put">> | undefined;

type Calls = { decisions: DecideAgentApprovalReq[]; sent: string[] };

/** 最小的控制器：审批卡只用 decide / stop，消息流里的行只用 state.approvals / pending / busy / send */
function fakeCtl(
  a: AgentApprovalDto,
  calls: Calls,
  busy = false,
  actionable = a.status === "pending",
): AgentController {
  return {
    busy,
    pending: actionable ? a : null,
    state: { approvals: { [a.id]: a } },
    decide: async (_id: string, req: DecideAgentApprovalReq) => {
      calls.decisions.push(req);
      return "ok";
    },
    send: async (text: string) => {
      calls.sent.push(text);
      return true;
    },
    stop: async () => {},
  } as unknown as AgentController;
}

async function mountDock(a: AgentApprovalDto, decide?: AgentController["decide"]) {
  const container = win.document.createElement("div");
  win.document.body.appendChild(container);
  const calls: Calls = { decisions: [], sent: [] };
  let root: Root;
  await act(async () => {
    root = createRoot(container);
    const ctl = fakeCtl(a, calls);
    if (decide) ctl.decide = decide;
    root.render(<ApprovalCard approval={a} ctl={ctl} />);
  });
  return { container, calls, unmount: () => act(async () => root.unmount()) };
}

async function mountRow(a: AgentApprovalDto, busy = false, actionable?: boolean) {
  const container = win.document.createElement("div");
  win.document.body.appendChild(container);
  const calls: Calls = { decisions: [], sent: [] };
  let root: Root;
  await act(async () => {
    root = createRoot(container);
    root.render(<ApprovalRow approvalId={a.id} ctl={fakeCtl(a, calls, busy, actionable)} />);
  });
  return { container, calls, unmount: () => act(async () => root.unmount()) };
}

const click = (el: Element | null | undefined) =>
  act(async () => {
    (el as HTMLElement | null | undefined)?.click();
  });

/** Base UI 的复选框把点击转给旁边隐藏的原生 input，直接点它和真实点击落到同一个处理函数 */
const toggle = (container: HTMLElement, label: string) =>
  click(container.querySelector(`[aria-label="选择 ${label}"]`)?.nextElementSibling as HTMLElement);

/** 按文字找决定选项（按钮里文字前有序号）；exact 要求整句相同，免得「拒绝」匹配到「拒绝，并告诉…」 */
const option = (container: HTMLElement, text: string, exact = false) =>
  [...container.querySelectorAll<HTMLButtonElement>('button[role="option"]')].find((b) => {
    const label = (b.textContent ?? "").replace(/^\d+/, "").trim();
    return exact ? label === text : label.startsWith(text);
  });

const secretApproval = (exists = true): AgentApprovalDto =>
  approval([], {
    kind: "admin_secret",
    payload: {
      channel_key: "cloud",
      channel_label: "云平台",
      exists,
      secret_configured: false,
      reason: "接入渠道",
      required_role: "super_admin",
    },
  });

const trialApproval = (): AgentApprovalDto =>
  approval([], {
    kind: "admin_test_run",
    payload: {
      model_key: "video",
      model_label: "视频模型",
      input_summary: "t2v · 5s · 720p",
      estimate: { credits: 5, yuan: 0.5 },
      attempt: 2,
      attempts_left: 1,
      reason: "验证插件",
      required_role: "admin",
    },
  });

const button = (container: HTMLElement, label: string) =>
  [...container.querySelectorAll("button")].find((b) => b.textContent?.trim() === label);

async function typeSecret(container: HTMLElement, value: string) {
  const input = container.querySelector<HTMLInputElement>('input[type="password"]');
  expect(input).not.toBeNull();
  await act(async () => {
    Object.getOwnPropertyDescriptor(win.HTMLInputElement.prototype, "value")!.set!.call(
      input,
      value,
    );
    input!.dispatchEvent(new win.Event("input", { bubbles: true }));
  });
  return input!;
}

describe("二期变更单明细", () => {
  test("插件、渠道、模型新建摘要、验证结果与可展开代码差异", async () => {
    useAdminStore.setState({ role: "super_admin" });
    const m = await mountDock(
      approval([
        item("插件", {
          target: "plugin",
          detail: {
            plugin_key: "p",
            version_before: "1",
            version_after: "2",
            meta: { name: "协议", vendor: "厂商", auth: "bearer", endpoints: "/videos" },
            code_lines: 10,
            code_diff: "-old()\n+new()",
            check: {
              ok: false,
              request: { method: "POST", url: "https://cloud/videos" },
              issues: ["解析响应失败"],
            },
          },
        }),
        item("渠道", {
          target: "channel",
          depends_on: [0],
          detail: { base_url: "https://cloud", headers: ["X-Client"], secret: "missing" },
        }),
        item("新模型", {
          target: "model_create",
          depends_on: [1],
          detail: {
            kind: "video",
            channel: "cloud",
            upstream_model: "video-v2",
            plugin: "p@latest",
          },
        }),
      ]),
    );
    for (const text of [
      "1 → 2",
      "厂商",
      "bearer",
      "/videos",
      "POST",
      "https://cloud/videos",
      "解析响应失败",
      "X-Client",
      "未配置密钥",
      "video-v2",
      "p@latest",
    ])
      expect(m.container.textContent).toContain(text);
    expect(m.container.textContent).not.toContain("old()");
    await click(button(m.container, "代码差异"));
    expect(m.container.textContent).toContain("old()");
    const code = m.container.querySelector("pre[aria-label='插件代码差异']");
    expect(code?.classList.contains("font-mono")).toBe(true);
    expect(code?.querySelector(".text-destructive")?.textContent).toBe("-old()");
    expect(code?.querySelector(".text-status-success")?.textContent).toBe("+new()");
    await toggle(m.container, "插件");
    for (const label of ["渠道", "新模型"]) {
      const box = m.container.querySelector(`[aria-label="选择 ${label}"]`);
      expect(box?.getAttribute("aria-checked")).toBe("false");
      expect((box?.nextElementSibling as HTMLInputElement)?.disabled).toBe(true);
    }
    expect(m.container.textContent).toContain("依赖第 1 项");
    await m.unmount();
  });

  test("新插件显示行数及静态验证通过", async () => {
    const m = await mountDock(
      approval([
        item("p", {
          target: "plugin",
          detail: {
            plugin_key: "p",
            version_before: null,
            version_after: "1",
            meta: { name: "协议", auth: "none", endpoints: "/v1" },
            code_lines: 12,
            code_diff: "",
            check: { ok: true, issues: [] },
          },
        }),
      ]),
    );
    expect(m.container.textContent).toContain("新插件 · 12 行");
    expect(m.container.textContent).toContain("静态验证通过");
    await m.unmount();
  });
});

describe("密钥卡片", () => {
  test("先保存渠道密钥，成功后才决定；提交立即清空，store 和决定不含密钥", async () => {
    useAdminStore.setState({ role: "super_admin" });
    let complete!: (value: unknown) => void;
    secretRequest = spyOn(service, "put").mockImplementation(
      () =>
        new Promise((resolve) => {
          complete = resolve;
        }),
    );
    const m = await mountDock(secretApproval());
    const key = "p2-local-secret-test-only";
    const input = await typeSecret(m.container, key);
    expect(input.autocomplete).toBe("off");
    expect(JSON.stringify(useAgentStore.getState())).not.toContain(key);
    expect(JSON.stringify(useAdminStore.getState())).not.toContain(key);
    await click(button(m.container, "保存"));
    expect(secretRequest).toHaveBeenCalledWith("/admin/ai/channels/cloud/secret", { value: key });
    expect(input.value).toBe("");
    expect(m.calls.decisions).toEqual([]);
    await act(async () => complete(undefined));
    expect(m.calls.decisions).toEqual([{ decision: "approve", answer: "configured" }]);
    expect(JSON.stringify(m.calls)).not.toContain(key);
    expect(JSON.stringify(useAgentStore.getState())).not.toContain(key);
    expect(m.container.textContent).not.toContain(key);
    await m.unmount();
  });

  test("保存失败不决定、清空密码且不回显异常中的密钥", async () => {
    useAdminStore.setState({ role: "super_admin" });
    const key = "p2-secret-in-error";
    secretRequest = spyOn(service, "put").mockRejectedValue(new Error(key));
    const m = await mountDock(secretApproval());
    const input = await typeSecret(m.container, key);
    await click(button(m.container, "保存"));
    expect(m.calls.decisions).toEqual([]);
    expect(input.value).toBe("");
    expect(m.container.textContent).toContain("密钥保存失败");
    expect(m.container.textContent).not.toContain(key);
    await m.unmount();
  });

  test("密钥保存成功但决定失败时给出就地反馈", async () => {
    useAdminStore.setState({ role: "super_admin" });
    secretRequest = spyOn(service, "put").mockResolvedValue(undefined);
    const m = await mountDock(secretApproval(), async () => "failed");
    const input = await typeSecret(m.container, "test-secret");
    await click(button(m.container, "保存"));
    expect(input.value).toBe("");
    expect(m.container.textContent).toContain("密钥已保存，确认失败");
    await m.unmount();
  });

  test("渠道不存在或权限不足时禁用输入和保存，仍可跳过", async () => {
    for (const exists of [false, true]) {
      const m = await mountDock(secretApproval(exists));
      expect(m.container.querySelector<HTMLInputElement>('input[type="password"]')?.disabled).toBe(
        true,
      );
      expect(button(m.container, "保存")?.disabled).toBe(true);
      expect(m.container.textContent).toContain(exists ? "需 super_admin" : "先应用建渠道的变更单");
      await click(button(m.container, "跳过"));
      expect(m.calls.decisions).toEqual([{ decision: "reject" }]);
      await m.unmount();
    }
  });

  test("时间线只显示保存或跳过，不显示 answer", async () => {
    for (const configured of [true, false]) {
      const m = await mountRow({
        ...secretApproval(),
        status: configured ? "executed" : "rejected",
        decision: { answer: "must-never-be-shown" },
        result: { configured },
      });
      expect(m.container.textContent).toContain(configured ? "已保存密钥" : "已跳过");
      expect(m.container.textContent).not.toContain("must-never-be-shown");
      await m.unmount();
    }
  });
});

describe("试跑卡片和时间线", () => {
  test("缺少成本报价时显示无法估算，不伪造零费用", async () => {
    const a = trialApproval();
    a.payload = { ...(a.payload as AdminTestRunPayload), estimate: { credits: null, yuan: null } };
    const m = await mountDock(a);
    expect(m.container.textContent).toContain("暂无法估算");
    expect(m.container.textContent).not.toContain("0 积分");
    await m.unmount();
  });
  test("显示摘要、费用示意、次数，批准与拒绝不附加输入", async () => {
    for (const approve of [true, false]) {
      const m = await mountDock(trialApproval());
      for (const text of [
        "视频模型",
        "t2v · 5s · 720p",
        "示意",
        "5 积分",
        "¥0.5",
        "第 2 次 / 共 3 次",
      ])
        expect(m.container.textContent).toContain(text);
      await click(option(m.container, approve ? "同意试跑" : "不试跑", true));
      expect(m.calls.decisions).toEqual([{ decision: approve ? "approve" : "reject" }]);
      await m.unmount();
    }
  });

  test("试跑结果含状态、耗时、分类、产物和 trace 链接；运行中不误报成功", async () => {
    for (const status of ["succeeded", "failed", "timeout", undefined] as const) {
      const m = await mountRow({
        ...trialApproval(),
        status: status ? "executed" : "approved",
        result: status
          ? {
              test_run_id: "t1",
              status,
              duration_ms: 1500,
              error_class: "upstream",
              error: status === "succeeded" ? undefined : "上游响应失败",
              trace_url: "/admin/trace/t1",
              output_url: "https://cloud/video.mp4",
            }
          : null,
      });
      expect(m.container.textContent).toContain(
        status === "succeeded" ? "成功" : status === "failed" ? "失败" : "仍在运行",
      );
      if (status) {
        expect(m.container.textContent).toContain("1.5s");
        expect(m.container.textContent).toContain("upstream");
        expect(m.container.querySelector('a[href="/admin/trace/t1"]')?.textContent).toBe(
          "查看 trace",
        );
        expect(m.container.querySelector('a[href="https://cloud/video.mp4"]')).not.toBeNull();
      }
      await m.unmount();
    }
  });

  test("超时不是失败：后端等满十分钟就交回结果，上游任务仍在跑、没有被取消", async () => {
    const m = await mountRow({
      ...trialApproval(),
      status: "executed",
      result: { test_run_id: "t1", status: "timeout" },
    });
    const text = m.container.textContent ?? "";
    expect(text).toContain("仍在运行");
    expect(text).not.toContain("失败");
    expect(m.container.querySelector(".text-destructive")).toBeNull();
    await m.unmount();
  });

  test("结果链接只允许站内地址或 http(s)", async () => {
    const m = await mountRow({
      ...trialApproval(),
      status: "executed",
      result: {
        test_run_id: "t1",
        status: "succeeded",
        trace_url: "javascript:alert(1)",
        output_url: "data:text/html,x",
      },
    });
    expect(m.container.querySelector("a")).toBeNull();
    await m.unmount();
  });
});

describe("共用审批卡：画布与短剧回归", () => {
  test("生成、删除、提问和短剧修改继续提交各自的决定", async () => {
    const cases: { a: AgentApprovalDto; label: string; expected: DecideAgentApprovalReq }[] = [
      {
        a: approval([], {
          kind: "generate",
          payload: {
            reason: "生成画面",
            items: [
              {
                node_id: "n1",
                label: "画面",
                model_key: "m1",
                model_name: "模型",
                price: 0,
                count: 1,
              },
            ],
          },
        }),
        label: "批准生成",
        expected: { decision: "approve", items: [{ index: 0, approve: true, count: 1 }] },
      },
      {
        a: approval([], {
          kind: "delete",
          payload: {
            reason: "清理画布",
            node_ids: ["n1"],
            labels: ["画面"],
            outputs: [false],
            edge_ids: [],
          },
        }),
        label: "删除 1 项",
        expected: { decision: "approve", items: [{ index: 0, approve: true }] },
      },
      {
        a: approval([], {
          kind: "ask",
          payload: { question: "选择画幅", kind: "choice", options: ["横屏", "竖屏"] },
        }),
        label: "横屏",
        expected: { decision: "approve", answer: "横屏" },
      },
      {
        a: approval([], {
          kind: "edit",
          payload: { title: "修改剧本", before: "原结尾", after: "新结尾" },
        }),
        label: "批准修改",
        expected: { decision: "approve" },
      },
    ];
    for (const { a, label, expected } of cases) {
      const m = await mountDock(a);
      await click(option(m.container, label));
      expect(m.calls.decisions).toEqual([expected]);
      expect(m.container.querySelector('input[type="password"]')).toBeNull();
      await m.unmount();
    }
  });
});

describe("审批卡：变更单内容", () => {
  test("单号、条数、理由、逐项名称和「当前 → 变更后」", async () => {
    const m = await mountDock(approval([item("a"), item("b")]));
    const text = m.container.textContent ?? "";
    expect(text).toContain("调价");
    expect(text).toContain("CO-1");
    expect(text).toContain("2 项");
    expect(text).toContain("按成本加价");
    const diff = m.container.querySelector('[aria-label="当前到变更后"]');
    expect(diff?.textContent).toContain("pricing.per_second");
    expect(diff?.textContent).toContain("8");
    expect(diff?.textContent).toContain("→");
    expect(diff?.textContent).toContain("10");
    await m.unmount();
  });

  test("校验问题、警告显示出来；有问题的条目不能勾选；角色不够的写明需要的角色", async () => {
    const m = await mountDock(
      approval([
        item("a"),
        item("bad", { status: "invalid", issues: ["价格为负"] }),
        item("hi", { required_role: "super_admin", warnings: ["影响所有用户"] }),
      ]),
    );
    const text = m.container.textContent ?? "";
    expect(text).toContain("价格为负");
    expect(text).toContain("影响所有用户");
    expect(text).toContain("需 super_admin");
    const box = (label: string) =>
      m.container.querySelector(`[aria-label="选择 ${label}"]`)?.nextElementSibling as
        | HTMLInputElement
        | undefined;
    expect(box("bad")?.disabled).toBe(true);
    expect(box("hi")?.disabled).toBe(true);
    expect(box("a")?.disabled).toBe(false);
    expect(option(m.container, "应用 1 项变更")).toBeTruthy();
    await m.unmount();
  });

  test("super_admin 可以勾选需要 super_admin 的条目", async () => {
    useAdminStore.setState({ role: "super_admin" });
    const m = await mountDock(approval([item("hi", { required_role: "super_admin" })]));
    expect(option(m.container, "应用 1 项变更")).toBeTruthy();
    await m.unmount();
  });

  test("计算依据默认收起，点开后显示键值", async () => {
    const m = await mountDock(
      approval([
        item("a", {
          basis: [{ path: "pricing.per_second", mode: "relative", from: 8, pct: 25, price: 10 }],
        }),
      ]),
    );
    expect(m.container.textContent).not.toContain("原价");
    await click(
      [...m.container.querySelectorAll("button")].find((b) => b.textContent === "计算依据"),
    );
    expect(m.container.textContent).toContain("原价");
    expect(m.container.textContent).toContain("+25%");
    await m.unmount();
  });
});

describe("审批卡：勾选与决定", () => {
  test("取消勾选一项：按钮文字跟着变，partial 请求体里每项都有 approve", async () => {
    const m = await mountDock(approval([item("a"), item("b"), item("c")]));
    expect(option(m.container, "应用 3 项变更")).toBeTruthy();
    await toggle(m.container, "b");
    expect(option(m.container, "应用 2 项变更")).toBeTruthy();
    await click(option(m.container, "应用 2 项变更"));
    expect(m.calls.decisions).toEqual([
      {
        decision: "approve",
        items: [
          { index: 0, approve: true },
          { index: 1, approve: false },
          { index: 2, approve: true },
        ],
      },
    ]);
    await m.unmount();
  });

  test("没有实际改动的条目默认不勾选", async () => {
    const m = await mountDock(
      approval([item("a"), item("same", { diff: [{ path: "p", before: 5, after: 5 }] })]),
    );
    expect(option(m.container, "应用 1 项变更")).toBeTruthy();
    await m.unmount();
  });

  test("全部取消勾选后「应用」不可点", async () => {
    const m = await mountDock(approval([item("a")]));
    await toggle(m.container, "a");
    const apply = option(m.container, "应用 0 项变更");
    expect(apply?.disabled).toBe(true);
    await click(apply);
    expect(m.calls.decisions).toEqual([]);
    await m.unmount();
  });

  test("拒绝：只带 decision；「拒绝，并告诉助手怎么改」带 answer", async () => {
    const m = await mountDock(approval([item("a")]));
    await click(option(m.container, "拒绝", true));
    expect(m.calls.decisions).toEqual([{ decision: "reject" }]);
    await m.unmount();

    const n = await mountDock(approval([item("a")]));
    await click(option(n.container, "拒绝，并告诉助手"));
    const input = n.container.querySelector("form input") as HTMLInputElement;
    expect(input).toBeTruthy();
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(win.HTMLInputElement.prototype, "value")!.set!;
      setter.call(input, "加价率太低");
      input.dispatchEvent(new win.Event("input", { bubbles: true }));
    });
    await act(async () => {
      n.container
        .querySelector("form")!
        .dispatchEvent(new win.Event("submit", { bubbles: true, cancelable: true }));
    });
    expect(n.calls.decisions).toEqual([{ decision: "reject", answer: "加价率太低" }]);
    await n.unmount();
  });
});

describe("消息流里的变更单", () => {
  const applied = (over: Partial<AgentApprovalDto> = {}) =>
    approval([item("a"), item("b")], {
      status: "partially_approved",
      result: {
        items: [
          { index: 0, status: "applied" },
          { index: 1, status: "failed", message: "渠道不可用" },
        ],
        applied_by: "zhang",
        applied_at: "2026-11-02T06:00:00Z",
        audit_ids: [3],
      },
      ...over,
    });

  test("待审核且运行停在它上面：直接是可操作的审批卡，不是一行记录", async () => {
    const m = await mountRow(approval([item("a")]));
    expect(m.container.querySelector("[role=group][aria-label=调价]")).not.toBeNull();
    expect(option(m.container, "应用 1 项变更")).toBeTruthy();
    expect(m.container.textContent).not.toContain("回滚");
    await click(option(m.container, "应用 1 项变更"));
    expect(m.calls.decisions).toEqual([
      { decision: "approve", items: [{ index: 0, approve: true }] },
    ]);
    await m.unmount();
  });

  test("待审核但运行已结束（作废的单）：只读的一行，不能操作", async () => {
    const m = await mountRow(approval([item("a")]), false, false);
    expect(m.container.textContent).toContain("调价");
    expect(m.container.textContent).toContain("等你确认");
    expect(m.container.querySelector("button[role=option]")).toBeNull();
    await m.unmount();
  });

  test("决定之后：已应用 / 失败条数、操作人、时间", async () => {
    const m = await mountRow(applied());
    const text = m.container.textContent ?? "";
    expect(text).toContain("CO-1");
    expect(text).toContain("已应用 1 项");
    expect(text).toContain("失败 1 项");
    expect(text).toContain("zhang");
    expect(text).toMatch(/\d{2}:\d{2}/);
    await m.unmount();
  });

  test("点「回滚」：发消息「回滚变更单 <单号>」", async () => {
    const m = await mountRow(applied());
    const button = [...m.container.querySelectorAll("button")].find((b) =>
      b.textContent?.includes("回滚"),
    );
    await click(button);
    expect(m.calls.sent).toEqual(["回滚变更单 CO-1"]);
    await m.unmount();
  });

  test("运行中回滚不可点；一项都没应用、被拒绝的单没有回滚", async () => {
    const busy = await mountRow(applied(), true);
    const button = [...busy.container.querySelectorAll("button")].find((b) =>
      b.textContent?.includes("回滚"),
    ) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    await busy.unmount();

    const none = await mountRow(
      applied({
        result: {
          items: [{ index: 0, status: "failed" }],
          applied_by: "z",
          applied_at: "",
          audit_ids: [],
        } as never,
      }),
    );
    expect(none.container.textContent).not.toContain("回滚");
    await none.unmount();

    const rejected = await mountRow(approval([item("a")], { status: "rejected" }));
    expect(rejected.container.textContent).toContain("已拒绝");
    expect(rejected.container.textContent).not.toContain("回滚");
    await rejected.unmount();
  });
});

const checkboxLabels = (container: HTMLElement) =>
  [...container.querySelectorAll('[aria-label^="选择 "]')].map((el) =>
    el.getAttribute("aria-label"),
  );

const moreButton = (container: HTMLElement) =>
  [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("还有"));

describe("审批卡：条目多时折叠", () => {
  test("变更单超过 3 项：只露前 3 项，「还有 N 项」点开展开；折叠的项照样在批准范围里", async () => {
    const m = await mountDock(approval(["a", "b", "c", "d", "e"].map((k) => item(k))));
    expect(checkboxLabels(m.container)).toEqual(["选择 a", "选择 b", "选择 c"]);
    expect(moreButton(m.container)?.textContent).toContain("还有 2 项");
    // 折叠着也能批准：5 项都在批准范围里
    expect(option(m.container, "应用 5 项变更")).toBeTruthy();
    await toggle(m.container, "b");
    await click(moreButton(m.container));
    expect(checkboxLabels(m.container)).toHaveLength(5);
    expect(moreButton(m.container)).toBeUndefined();
    await click(option(m.container, "应用 4 项变更"));
    expect(m.calls.decisions).toEqual([
      {
        decision: "approve",
        items: [
          { index: 0, approve: true },
          { index: 1, approve: false },
          { index: 2, approve: true },
          { index: 3, approve: true },
          { index: 4, approve: true },
        ],
      },
    ]);
    await m.unmount();
  });

  test("正好 3 项不折叠，没有「还有」按钮", async () => {
    const m = await mountDock(approval(["a", "b", "c"].map((k) => item(k))));
    expect(checkboxLabels(m.container)).toHaveLength(3);
    expect(moreButton(m.container)).toBeUndefined();
    await m.unmount();
  });

  test("生成审批超过 3 个节点：只露前 3 个，展开后取消勾选的那项不在批准里", async () => {
    const gen = (n: number): AgentApprovalDto =>
      approval([], {
        kind: "generate",
        payload: {
          reason: "批量生成",
          items: Array.from({ length: n }, (_, i) => ({
            node_id: `n${i}`,
            label: `画面${i}`,
            model_key: "m1",
            model_name: "模型",
            price: 0,
            count: 1,
          })),
        },
      });
    const m = await mountDock(gen(5));
    const boxes = () => m.container.querySelectorAll('[aria-label^="生成「"]').length;
    expect(boxes()).toBe(3);
    expect(m.container.textContent).not.toContain("画面4");
    await click(moreButton(m.container));
    expect(boxes()).toBe(5);
    await click(m.container.querySelector('[aria-label="生成「画面4」"]')?.nextElementSibling);
    await click(option(m.container, "批准生成"));
    expect(m.calls.decisions[0]).toEqual({
      decision: "approve",
      items: [0, 1, 2, 3, 4].map((index) => ({ index, approve: index !== 4, count: 1 })),
    });
    await m.unmount();
  });
});

describe("变更单执行后的结果卡", () => {
  const field = (path: string, before: unknown, after: unknown) => ({ path, before, after });
  const done = (over: Partial<AgentApprovalDto> = {}) =>
    approval(
      [
        item("kling", {
          label: "可灵",
          diff: [field("pricing.per_second", 8, 10), field("tags[1]", "旧标签", null)],
        }),
        item("pricing", {
          target: "pricing_settings",
          label: "定价设置",
          diff: [field("default_markup_pct", null, 25)],
        }),
        item("new-api-video", {
          target: "plugin",
          label: "视频插件",
          diff: [field("version", "1", "2")],
        }),
        item("bad", { label: "坏模型" }),
        item("same", { label: "没勾选的" }),
      ],
      {
        status: "partially_approved",
        result: {
          items: [
            { index: 0, status: "applied" },
            { index: 1, status: "applied" },
            { index: 2, status: "applied" },
            { index: 3, status: "failed", message: "渠道不可用" },
            { index: 4, status: "rejected" },
          ],
          applied_by: "zhang",
          applied_at: "2026-11-02T06:00:00Z",
          audit_ids: [1, 2, 3],
        },
        ...over,
      },
    );

  test("按页面分组，对象标题是指向后台对应页面的链接", async () => {
    const m = await mountRow(done());
    const sections = [...m.container.querySelectorAll("section")].map((s) =>
      s.getAttribute("aria-label"),
    );
    expect(sections).toEqual(["模型配置", "定价设置", "插件"]);
    const links = [...m.container.querySelectorAll("a")].map((a) => [
      a.textContent,
      a.getAttribute("href"),
    ]);
    expect(links).toEqual([
      ["可灵", "/admin/ai/channels?model=kling"],
      ["定价设置", "/admin/settings/pricing"],
      ["视频插件", "/admin/ai/plugins"],
    ]);
    await m.unmount();
  });

  test("逐字段列改前 → 改后：改动、新增、删除各自的样子", async () => {
    const m = await mountRow(done());
    const rows = [...m.container.querySelectorAll('[aria-label="改前到改后"] > div')];
    const text = (i: number) => rows[i].textContent;
    expect(text(0)).toContain("pricing.per_second");
    expect(text(0)).toContain("8");
    expect(text(0)).toContain("→");
    expect(text(0)).toContain("10");
    expect(rows[0].querySelector(".line-through")?.textContent).toBe("8");
    expect(rows[0].querySelector(".text-status-success")?.textContent).toBe("10");
    // 删除：只有红色删除线的旧值
    expect(rows[1].querySelector(".text-destructive.line-through")?.textContent).toBe("旧标签");
    expect(rows[1].querySelector(".text-status-success")).toBeNull();
    // 新增：只有绿色的新值，没有删除线
    expect(rows[2].querySelector(".line-through")).toBeNull();
    expect(rows[2].querySelector(".text-status-success")?.textContent).toBe("25");
    await m.unmount();
  });

  test("失败和你没勾选的条目单独列出，写明原因", async () => {
    const m = await mountRow(done());
    const list = m.container.querySelector('[aria-label="未应用的条目"]');
    expect(list?.textContent).toContain("失败：坏模型 · 渠道不可用");
    expect(list?.textContent).toContain("未应用：没勾选的");
    expect(list?.querySelector(".text-destructive")?.textContent).toContain("坏模型");
    await m.unmount();
  });

  test("已应用超过 3 项时折叠，展开后全部可见", async () => {
    const items = ["a", "b", "c", "d"].map((k) => item(k, { label: `模型${k}` }));
    const m = await mountRow(
      approval(items, {
        status: "executed",
        result: {
          items: items.map((_, index) => ({ index, status: "applied" })),
          applied_by: "z",
          applied_at: "",
          audit_ids: [],
        } as never,
      }),
    );
    expect(m.container.textContent).toContain("模型c");
    expect(m.container.textContent).not.toContain("模型d");
    expect(moreButton(m.container)?.textContent).toContain("还有 1 项");
    await click(moreButton(m.container));
    expect(m.container.textContent).toContain("模型d");
    await m.unmount();
  });

  test("头部写明单号、条数、操作人，右上角「回滚」发出回滚消息", async () => {
    const m = await mountRow(done());
    const text = m.container.textContent ?? "";
    expect(text).toContain("CO-1");
    expect(text).toContain("已应用 3 项");
    expect(text).toContain("失败 1 项");
    expect(text).toContain("zhang");
    await click(
      [...m.container.querySelectorAll("button")].find((b) => b.textContent?.includes("回滚")),
    );
    expect(m.calls.sent).toEqual(["回滚变更单 CO-1"]);
    await m.unmount();
  });

  test("回滚单自己不能再回滚；一项都没应用时标题不报成功", async () => {
    const revert = await mountRow(
      approval([item("a")], {
        status: "executed",
        payload: {
          order_no: "CO-2",
          title: "回滚",
          reason: "",
          revert_of: "CO-1",
          items: [item("a")],
        } as never,
        result: {
          items: [{ index: 0, status: "applied" }],
          applied_by: "z",
          applied_at: "",
          audit_ids: [],
        } as never,
      }),
    );
    expect(revert.container.textContent).not.toContain("回滚变更单");
    expect(
      [...revert.container.querySelectorAll("button")].some((b) => b.textContent?.includes("回滚")),
    ).toBe(false);
    await revert.unmount();
  });
});
