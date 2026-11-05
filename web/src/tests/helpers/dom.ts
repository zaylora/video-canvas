import { mock } from "bun:test";
import type { act as ReactAct } from "react";
import type { Root, createRoot as CreateRoot } from "react-dom/client";

/**
 * 用 jsdom 挂载真实组件的测试工具：把 jsdom 的全局对象临时装到 globalThis 上，
 * 派发点击和输入，测完还原。每个测试文件在 beforeAll 里 install、afterAll 里 restore。
 */

const GLOBALS = [
  "window",
  "document",
  "navigator",
  "localStorage",
  "sessionStorage",
  "Node",
  "Element",
  "HTMLElement",
  "HTMLInputElement",
  "HTMLTextAreaElement",
  "HTMLButtonElement",
  "HTMLFormElement",
  "HTMLSelectElement",
  "HTMLIFrameElement",
  "SVGElement",
  "DocumentFragment",
  "MutationObserver",
  "Event",
  "MouseEvent",
  "PointerEvent",
  "KeyboardEvent",
  "FocusEvent",
  "InputEvent",
  "CustomEvent",
  "getComputedStyle",
  "requestAnimationFrame",
  "cancelAnimationFrame",
];

/** 装好的 DOM 环境 */
export interface DomEnv {
  /** jsdom 的 window */
  win: Window & typeof globalThis;
  /** React 的 act */
  act: typeof ReactAct;
  /** React 18+ 的 createRoot */
  createRoot: typeof CreateRoot;
  /** 还原全局对象 */
  restore: () => void;
  /**
   * 挂载一个元素到 document.body 下的新容器
   * @param element 要渲染的 React 元素
   */
  mount: (element: React.ReactElement) => Promise<{
    container: HTMLElement;
    unmount: () => Promise<void>;
  }>;
  /**
   * 点击元素（包在 act 里）
   * @param el 要点击的元素
   */
  click: (el: Element | null | undefined) => Promise<void>;
  /**
   * 往受控输入框 / 文本域里写字：绕过 React 对 value 的拦截，再派发 input 事件
   * @param el 输入框或文本域
   * @param value 写入的内容
   */
  type: (el: Element | null | undefined, value: string) => Promise<void>;
  /** 等所有挂起的 promise / 定时器推进一轮（包在 act 里） */
  flush: (ms?: number) => Promise<void>;
}

/** 安装 jsdom 全局环境 */
export async function installDom(): Promise<DomEnv> {
  const { JSDOM } = await import("jsdom");
  const jsdom = new JSDOM("<!doctype html><html><body></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  const win = jsdom.window as unknown as Window & typeof globalThis;
  const g = globalThis as unknown as Record<string, unknown>;
  const saved: Record<string, PropertyDescriptor | undefined> = {};
  for (const key of GLOBALS) {
    saved[key] = Object.getOwnPropertyDescriptor(g, key);
    Object.defineProperty(g, key, {
      value: (jsdom.window as unknown as Record<string, unknown>)[key],
      configurable: true,
      writable: true,
    });
  }
  /** jsdom 没有的浏览器 API：给个最小实现，够组件挂载即可 */
  const extras: Record<string, unknown> = {
    ResizeObserver: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
    matchMedia: () => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
    }),
  };
  for (const [key, value] of Object.entries(extras)) {
    saved[key] = Object.getOwnPropertyDescriptor(g, key);
    Object.defineProperty(g, key, { value, configurable: true, writable: true });
    (win as unknown as Record<string, unknown>)[key] = value;
  }
  // jsdom 不做布局，所有元素量出来都是 0×0：虚拟列表据此会一行都不渲染。给个固定的可视尺寸。
  for (const [key, value] of [
    ["clientWidth", 520],
    ["clientHeight", 320],
  ] as const) {
    Object.defineProperty(win.HTMLElement.prototype, key, { configurable: true, get: () => value });
  }
  (win.HTMLElement.prototype as { scrollIntoView?: () => void }).scrollIntoView = () => {};
  g.IS_REACT_ACT_ENVIRONMENT = true;
  const React = await import("react");
  // Base UI 在模块加载时判断 typeof document，没有 DOM 就把 useIsoLayoutEffect 定成空函数。
  // 同一个测试进程里，前面的测试文件（服务端渲染那类）可能已经在没有 DOM 的环境里加载过它，
  // 那样弹层、对话框永远打不开。这里把它换成真正的 useLayoutEffect。
  mock.module("@base-ui/utils/useIsoLayoutEffect", () => ({
    useIsoLayoutEffect: React.useLayoutEffect,
  }));
  const { act } = React;
  const { createRoot } = await import("react-dom/client");

  const restore = () => {
    for (const [key, desc] of Object.entries(saved)) {
      if (desc) Object.defineProperty(g, key, desc);
      else delete g[key];
    }
    delete g.IS_REACT_ACT_ENVIRONMENT;
  };

  return {
    win,
    act,
    createRoot,
    restore,
    async mount(element) {
      const container = win.document.createElement("div");
      win.document.body.appendChild(container);
      let root: Root;
      await act(async () => {
        root = createRoot(container);
        root.render(element);
      });
      return {
        container,
        unmount: () => act(async () => root.unmount()),
      };
    },
    click: (el) =>
      act(async () => {
        (el as HTMLElement | null | undefined)?.click();
      }),
    type: (el, value) =>
      act(async () => {
        const target = el as HTMLInputElement | HTMLTextAreaElement;
        const proto =
          target.tagName === "TEXTAREA"
            ? win.HTMLTextAreaElement.prototype
            : win.HTMLInputElement.prototype;
        Object.getOwnPropertyDescriptor(proto, "value")?.set?.call(target, value);
        target.dispatchEvent(new win.Event("input", { bubbles: true }));
      }),
    flush: (ms = 0) =>
      act(async () => {
        await new Promise((resolve) => setTimeout(resolve, ms));
      }),
  };
}
