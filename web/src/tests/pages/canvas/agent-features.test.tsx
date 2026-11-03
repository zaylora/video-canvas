import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import type { act as ReactAct, createElement as CreateElement } from "react";
import type { Root, createRoot as CreateRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";

import type { AgentModelDto } from "@/api/agent/type";
import { AgentEmpty } from "@/pages/canvas/agent/agent-empty";
import { ContextChips } from "@/pages/canvas/agent/context-chips";
import type { AgentComposer as AgentComposerType } from "@/pages/canvas/agent/agent-composer";

/**
 * 画布组件新增的可选 props：缺省值保持画布现状，后台助手用它们关掉画布专属能力、显示页面上下文引用
 */

describe("AgentEmpty：副标题可选", () => {
  const starters = [{ icon: (() => null) as never, text: "核对价格" }];

  test("画布默认文案有副标题和键位提示", () => {
    const html = renderToStaticMarkup(<AgentEmpty onStarter={() => {}} />);
    expect(html).toContain("读懂画布、搭建节点、整理布局");
    expect(html).toContain("引用节点");
  });

  test("subtitle 为 null、hint 为 null：只有标题和起步建议", () => {
    const html = renderToStaticMarkup(
      <AgentEmpty
        onStarter={() => {}}
        copy={{ title: "让助手帮你调整后台配置", subtitle: null, starters, hint: null }}
      />,
    );
    expect(html).toContain("让助手帮你调整后台配置");
    expect(html).toContain("核对价格");
    expect(html).not.toContain("text-muted-foreground mb-4 text-center text-xs");
    expect(html).not.toContain("<kbd");
  });
});

describe("ContextChips", () => {
  const refs = [
    { id: "page", label: "模型渠道", title: "/admin/ai/channels" },
    { id: "model", label: "模型：kling-v2-1" },
  ];

  test("没有引用时什么都不渲染", () => {
    expect(renderToStaticMarkup(<ContextChips refs={[]} />)).toBe("");
  });

  test("只读形态：靠右对齐，没有移除按钮", () => {
    const html = renderToStaticMarkup(<ContextChips refs={refs} />);
    expect(html).toContain("模型：kling-v2-1");
    expect(html).toContain("justify-end");
    expect(html).not.toContain("<button");
  });

  test("可移除形态：每个 chip 有「移除 …」按钮", () => {
    const html = renderToStaticMarkup(<ContextChips refs={refs} onRemove={() => {}} />);
    expect(html).toContain('aria-label="移除 模型渠道"');
    expect(html).toContain('aria-label="移除 模型：kling-v2-1"');
  });
});

describe("AgentComposer：画布能力开关", () => {
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
  let createElement: typeof CreateElement;
  let AgentComposer: typeof AgentComposerType;
  let useAgentAttachments: typeof import("@/hooks/use-agent-attachments").useAgentAttachments;

  const models = [{ key: "m1", name: "助手模型", vision: true }] as AgentModelDto[];

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
    createRoot = (await import("react-dom/client")).createRoot;
    AgentComposer = (await import("@/pages/canvas/agent/agent-composer")).AgentComposer;
    useAgentAttachments = (await import("@/hooks/use-agent-attachments")).useAgentAttachments;
  });

  afterAll(() => {
    const g = globalThis as unknown as Record<string, unknown>;
    for (const key of GLOBALS) {
      Object.defineProperty(g, key, { value: saved[key], configurable: true, writable: true });
    }
    delete g.IS_REACT_ACT_ENVIRONMENT;
  });

  /** 挂载输入区，返回容器 */
  async function mount(extra: Partial<Parameters<typeof AgentComposerType>[0]> = {}) {
    const container = win.document.createElement("div");
    win.document.body.appendChild(container);
    function Host() {
      const attachments = useAgentAttachments();
      return createElement(AgentComposer, {
        text: "",
        onTextChange: () => {},
        onSend: () => {},
        onStop: () => {},
        busy: false,
        sending: false,
        models,
        modelKey: "m1",
        context: { tokens: 0, window: 1000 },
        selection: [],
        refNodes: [],
        editorRef: { current: null },
        nodes: [],
        attachments,
        vision: true,
        ...extra,
      });
    }
    let root: Root;
    await act(async () => {
      root = createRoot(container);
      root.render(createElement(Host));
    });
    return { container, unmount: () => act(async () => root.unmount()) };
  }

  const has = (container: HTMLElement, label: string) =>
    container.querySelector(`[aria-label="${label}"]`) !== null;

  test("缺省：附件、插入模型、插入技能都在（画布现状），占位文字是画布的", async () => {
    const m = await mount();
    expect(has(m.container, "添加附件")).toBe(true);
    expect(has(m.container, "插入模型")).toBe(true);
    expect(has(m.container, "插入技能")).toBe(true);
    expect(m.container.textContent).toContain("描述你的想法，@ 引用节点，/ 插入技能");
    await m.unmount();
  });

  test("features 全关：没有附件、插入模型、插入技能；占位文字、页面上下文引用换成后台的", async () => {
    const removed: string[] = [];
    const m = await mount({
      features: { attachments: false, mentions: false, models: false, skills: false },
      placeholder: "描述要调整的配置",
      contextRefs: [{ id: "model", label: "模型：kling-v2-1" }],
      onRemoveContextRef: (id) => removed.push(id),
    });
    expect(has(m.container, "添加附件")).toBe(false);
    expect(has(m.container, "插入模型")).toBe(false);
    expect(has(m.container, "插入技能")).toBe(false);
    // 模型选择和发送仍在
    expect(has(m.container, "Agent 模型")).toBe(true);
    expect(m.container.textContent).toContain("描述要调整的配置");
    expect(m.container.textContent).toContain("模型：kling-v2-1");
    await act(async () => {
      (m.container.querySelector('[aria-label="移除 模型：kling-v2-1"]') as HTMLElement).click();
    });
    expect(removed).toEqual(["model"]);
    await m.unmount();
  });
});
