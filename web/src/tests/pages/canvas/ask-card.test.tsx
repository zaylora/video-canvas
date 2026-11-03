import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import type { act as ReactAct } from "react";
import type { Root, createRoot as CreateRoot } from "react-dom/client";

import type {
  AgentApprovalDto,
  AgentAskPayload,
  AgentAskQuestion,
  DecideAgentApprovalReq,
} from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import type { ApprovalCard as ApprovalCardType } from "@/pages/canvas/agent/approval-card";
import type { ApprovalRow as ApprovalRowType } from "@/pages/canvas/agent/approval-row";

/**
 * 提问卡一次列多个问题：用 jsdom 挂载真实组件、派发点击和输入，
 * 检查表单的可提交条件、提交给后端的 answers / answer，以及答完 / 跳过后消息流里的记录行。
 */

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
});

afterAll(() => {
  const g = globalThis as unknown as Record<string, unknown>;
  for (const key of GLOBALS) {
    Object.defineProperty(g, key, { value: saved[key], configurable: true, writable: true });
  }
  delete g.IS_REACT_ACT_ENVIRONMENT;
});

const MODELS = [
  { key: "seedream", name: "Seedream", hint: "图片模型" },
  { key: "flux", name: "Flux", hint: "" },
];

const QUESTIONS: AgentAskQuestion[] = [
  {
    id: "model",
    question: "要接哪个模型？",
    kind: "choice",
    required: true,
    options: [{ label: "告诉你模型名称", follow: "如 kling-v3" }, { label: "价格页上的都接" }],
  },
  {
    id: "tags",
    question: "要哪些风格？",
    kind: "choice",
    required: true,
    multi: true,
    options: [{ label: "写实" }, { label: "动漫" }, { label: "水墨" }],
  },
  { id: "note", question: "还有补充吗？", kind: "text", required: false, placeholder: "随便写" },
];

const multi = (
  questions: AgentAskQuestion[] = QUESTIONS,
  over: Partial<AgentApprovalDto> = {},
): AgentApprovalDto => ({
  id: "a1",
  run_id: "r1",
  kind: "ask",
  status: "pending",
  payload: {
    question: `还差 ${questions.length} 件事要你定`,
    kind: questions[0].kind,
    options: questions[0].options?.map((o) => o.label),
    questions,
  } satisfies AgentAskPayload,
  quote_credits: 0,
  decision: null,
  expires_at: "2099-01-01T00:00:00Z",
  ...over,
});

/** 历史里的旧单题提问：没有 questions */
const legacy = (over: Partial<AgentApprovalDto> = {}): AgentApprovalDto => ({
  ...multi(),
  payload: { question: "画幅？", kind: "choice", options: ["16:9", "9:16"] },
  ...over,
});

type Calls = { decisions: DecideAgentApprovalReq[] };

function fakeCtl(a: AgentApprovalDto, calls: Calls, actionable: boolean): AgentController {
  return {
    busy: false,
    pending: actionable ? a : null,
    state: { approvals: { [a.id]: a } },
    decide: async (_id: string, req: DecideAgentApprovalReq) => {
      calls.decisions.push(req);
      return "ok";
    },
    send: async () => true,
    stop: async () => {},
  } as unknown as AgentController;
}

async function mount(kind: "card" | "row", a: AgentApprovalDto) {
  const container = win.document.createElement("div");
  win.document.body.appendChild(container);
  const calls: Calls = { decisions: [] };
  let root: Root;
  await act(async () => {
    root = createRoot(container);
    const ctl = fakeCtl(a, calls, kind === "card");
    root.render(
      kind === "card" ? (
        <ApprovalCard approval={a} ctl={ctl} />
      ) : (
        <ApprovalRow approvalId={a.id} ctl={ctl} />
      ),
    );
  });
  return { container, calls, unmount: () => act(async () => root.unmount()) };
}

const click = (el: Element | null | undefined) =>
  act(async () => {
    (el as HTMLElement | null | undefined)?.click();
  });

/** 往受控输入框里写字：绕过 React 对 value 的拦截，再派发 input 事件 */
const type = (el: Element | null | undefined, value: string) =>
  act(async () => {
    const input = el as HTMLInputElement;
    const set = Object.getOwnPropertyDescriptor(win.HTMLInputElement.prototype, "value")?.set;
    set?.call(input, value);
    input.dispatchEvent(new win.Event("input", { bubbles: true }));
  });

/** 在某一题（按问题文字找）里按选项文字找选项按钮 */
const choice = (container: HTMLElement, question: string, label: string) =>
  [
    ...container.querySelectorAll(
      `[role=group][aria-label="${question}"] [role=radio], [role=group][aria-label="${question}"] [role=checkbox]`,
    ),
  ].find((b) =>
    (b.textContent ?? "")
      .replace(/^[A-Z]/, "")
      .trim()
      .startsWith(label),
  );
const field = (container: HTMLElement, label: string) =>
  container.querySelector(`input[aria-label="${label}"]`);
const submitButton = (container: HTMLElement) =>
  [...container.querySelectorAll("button")].find((b) => b.textContent === "提交") as
    | HTMLButtonElement
    | undefined;
const progress = (container: HTMLElement) =>
  container.querySelector('[aria-label="进度"]')?.textContent;

describe("多题提问卡", () => {
  test("编号题目、A/B/C 选项、选填标记、进度，必填没答完不能提交", async () => {
    const m = await mount("card", multi());
    const text = m.container.textContent ?? "";
    expect(text).toContain("还差 3 件事要你定");
    expect(text).toContain("要接哪个模型？");
    expect(text).toContain("还有补充吗？");
    // 只有第三题标了选填
    expect(text.match(/选填/g)).toHaveLength(1);
    // 题号 1 2 3，选项字母 A B / A B C
    const radios = [...m.container.querySelectorAll("[role=radio],[role=checkbox]")].map((b) =>
      (b.textContent ?? "").slice(0, 1),
    );
    expect(radios).toEqual(["A", "B", "A", "B", "C"]);
    expect(progress(m.container)).toBe("0 / 3");
    expect(submitButton(m.container)?.disabled).toBe(true);

    await click(choice(m.container, "要接哪个模型？", "价格页上的都接"));
    expect(progress(m.container)).toBe("1 / 3");
    expect(submitButton(m.container)?.disabled).toBe(true); // 第二题必填还没答
    await click(choice(m.container, "要哪些风格？", "写实"));
    expect(progress(m.container)).toBe("2 / 3");
    expect(submitButton(m.container)?.disabled).toBe(false); // 选填的补充可以不写
    await m.unmount();
  });

  test("follow 输入框：选中带 follow 的选项才在原地展开，写了字才算答好", async () => {
    const m = await mount("card", multi());
    expect(field(m.container, "告诉你模型名称：补充")).toBeNull();
    await click(choice(m.container, "要接哪个模型？", "告诉你模型名称"));
    const input = field(m.container, "告诉你模型名称：补充") as HTMLInputElement;
    expect(input).not.toBeNull();
    expect(input.placeholder).toBe("如 kling-v3");
    expect(progress(m.container)).toBe("0 / 3"); // 选了却没写字，还不算答好
    await type(input, "kling-v3");
    expect(progress(m.container)).toBe("1 / 3");
    // 换选项后输入框收起
    await click(choice(m.container, "要接哪个模型？", "价格页上的都接"));
    expect(field(m.container, "告诉你模型名称：补充")).toBeNull();
    await m.unmount();
  });

  test("多选：可以同时选几项，再点一次取消", async () => {
    const m = await mount("card", multi());
    await click(choice(m.container, "要哪些风格？", "写实"));
    await click(choice(m.container, "要哪些风格？", "水墨"));
    const on = () =>
      [...m.container.querySelectorAll('[aria-label="要哪些风格？"] [role=checkbox]')].map((b) =>
        b.getAttribute("aria-checked"),
      );
    expect(on()).toEqual(["true", "false", "true"]);
    await click(choice(m.container, "要哪些风格？", "写实"));
    expect(on()).toEqual(["false", "false", "true"]);
    await m.unmount();
  });

  test("提交：answers 按题目 id，多选是数组，answer 是「问题：回答」用分号连起来，没答的选填不带", async () => {
    const m = await mount("card", multi());
    await click(choice(m.container, "要接哪个模型？", "告诉你模型名称"));
    await type(field(m.container, "告诉你模型名称：补充"), " kling-v3 ");
    await click(choice(m.container, "要哪些风格？", "写实"));
    await click(choice(m.container, "要哪些风格？", "水墨"));
    await click(submitButton(m.container));
    expect(m.calls.decisions).toEqual([
      {
        decision: "approve",
        // 带 follow 的选项：回答写成「选项：输入」，模型才知道用户选的是哪一项
        answer: "要接哪个模型？：告诉你模型名称：kling-v3；要哪些风格？：写实、水墨",
        answers: [
          { id: "model", value: "告诉你模型名称：kling-v3" },
          { id: "tags", value: ["写实", "水墨"] },
        ],
      },
    ]);
    await m.unmount();
  });

  test("填写题和选模型题：数字、链接不合格时不能提交，选模型的回答带 key", async () => {
    const qs: AgentAskQuestion[] = [
      { id: "n", question: "几秒？", kind: "number", required: true },
      { id: "u", question: "文档链接？", kind: "url", required: false },
      {
        id: "m",
        question: "用哪个模型？",
        kind: "model",
        required: true,
        model_kind: "image",
        models: MODELS,
      },
    ];
    const m = await mount("card", multi(qs));
    await click(choice(m.container, "用哪个模型？", "Seedream"));
    await type(field(m.container, "几秒？"), "abc");
    expect(field(m.container, "几秒？")?.getAttribute("aria-invalid")).toBe("true");
    expect(submitButton(m.container)?.disabled).toBe(true);
    await type(field(m.container, "几秒？"), "5.5");
    expect(submitButton(m.container)?.disabled).toBe(false);
    await type(field(m.container, "文档链接？"), "ftp://x.com");
    expect(submitButton(m.container)?.disabled).toBe(true); // 选填写了但不合格也拦住
    await type(field(m.container, "文档链接？"), "https://example.com/doc");
    await click(submitButton(m.container));
    expect(m.calls.decisions[0]).toEqual({
      decision: "approve",
      answer:
        "几秒？：5.5；文档链接？：https://example.com/doc；用哪个模型？：Seedream（模型 key：seedream）",
      answers: [
        { id: "n", value: "5.5" },
        { id: "u", value: "https://example.com/doc" },
        { id: "m", value: "seedream" },
      ],
    });
    await m.unmount();
  });
});

describe("单题提问", () => {
  test("历史里的旧提问（没有 questions）：点选即提交，只带 answer", async () => {
    const m = await mount("card", legacy());
    expect(submitButton(m.container)).toBeUndefined(); // 没有表单和提交按钮
    const second = [...m.container.querySelectorAll('button[role="option"]')][1];
    await click(second);
    expect(m.calls.decisions).toEqual([{ decision: "approve", answer: "9:16" }]);
    await m.unmount();
  });

  test("新的单题选择题：点选即提交，同时带 answers 和 answer", async () => {
    const one: AgentAskQuestion = {
      id: "ratio",
      question: "画幅？",
      kind: "choice",
      required: true,
      options: [{ label: "16:9" }, { label: "9:16" }],
    };
    const m = await mount("card", multi([one]));
    await click([...m.container.querySelectorAll('button[role="option"]')][0]);
    expect(m.calls.decisions).toEqual([
      { decision: "approve", answer: "16:9", answers: [{ id: "ratio", value: "16:9" }] },
    ]);
    await m.unmount();
  });

  test("单题的填写题走表单，answer 就是回答本身", async () => {
    const one: AgentAskQuestion = { id: "k", question: "叫什么？", kind: "text", required: true };
    const m = await mount("card", multi([one]));
    expect(progress(m.container)).toBe("0 / 1");
    await type(field(m.container, "叫什么？"), "小镜");
    await click(submitButton(m.container));
    expect(m.calls.decisions).toEqual([
      { decision: "approve", answer: "小镜", answers: [{ id: "k", value: "小镜" }] },
    ]);
    await m.unmount();
  });
});

describe("消息流里的记录行", () => {
  const decided = (
    answers: Array<[string, string]>,
    status: AgentApprovalDto["status"] = "approved",
  ) =>
    multi(QUESTIONS, {
      status,
      decision: {
        decision: "approve",
        answer: "略",
        answers: QUESTIONS.map((q, i) => ({
          id: q.id,
          question: q.question,
          answer: answers[i]?.[1] ?? "",
          value: answers[i]?.[1] ?? "",
        })),
      },
    });

  test("答完后每题一行「↳ 问题 回答」，没答的选填题写「没回答」", async () => {
    const m = await mount(
      "row",
      decided([
        ["model", "kling-v3"],
        ["tags", "写实、水墨"],
      ]),
    );
    const rows = [...m.container.querySelectorAll("[aria-label^='你的回答：']")];
    expect(rows.map((r) => r.textContent)).toEqual([
      "要接哪个模型？kling-v3",
      "要哪些风格？写实、水墨",
      "还有补充吗？没回答",
    ]);
    await m.unmount();
  });

  test("被跳过的多题提问收成一行「跳过了 N 个问题」", async () => {
    const m = await mount(
      "row",
      multi(QUESTIONS, { status: "rejected", decision: { decision: "reject", question_count: 3 } }),
    );
    expect(m.container.textContent).toContain("跳过了 3 个问题");
    expect(m.container.querySelectorAll("[aria-label='跳过了 3 个问题']")).toHaveLength(1);
    await m.unmount();
  });

  test("旧的单题提问：答过是一行，跳过仍是「跳过了这个问题」", async () => {
    const answered = await mount(
      "row",
      legacy({ status: "approved", decision: { decision: "approve", answer: "9:16" } }),
    );
    expect(answered.container.querySelector("[aria-label='你的回答：9:16']")?.textContent).toBe(
      "画幅？9:16",
    );
    await answered.unmount();
    const skipped = await mount(
      "row",
      legacy({ status: "rejected", decision: { decision: "reject" } }),
    );
    expect(skipped.container.textContent).toContain("跳过了这个问题");
    await skipped.unmount();
  });
});
