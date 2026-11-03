import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import type { act as ReactAct, createElement as CreateElement } from "react";
import type { useAdminConfigChanged as UseAdminConfigChanged } from "@/hooks/use-admin-config-changed";
import type { Root, createRoot as CreateRoot } from "react-dom/client";

/** admin-config-changed：变更单应用后，页面按受影响的对象重新拉数据。用 jsdom 挂载真实 hook、派发 window 事件 */

type Dom = {
  window: Window & typeof globalThis;
};

let dom: Dom;
/** React 和 hook 在全局 DOM 就绪后才加载：react-dom 在加载时就判断有没有 DOM */
let act: typeof ReactAct;
let createRoot: typeof CreateRoot;
let useAdminConfigChanged: typeof UseAdminConfigChanged;
let createElement: typeof CreateElement;
const saved: Record<string, unknown> = {};
const GLOBALS = [
  "window",
  "document",
  "navigator",
  "Node",
  "Element",
  "HTMLElement",
  "HTMLInputElement",
  "HTMLTextAreaElement",
  "HTMLButtonElement",
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

beforeAll(async () => {
  const { JSDOM } = await import("jsdom");
  const jsdom = new JSDOM("<!doctype html><html><body></body></html>", { pretendToBeVisual: true });
  dom = { window: jsdom.window as unknown as Window & typeof globalThis };
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
  useAdminConfigChanged = (await import("@/hooks/use-admin-config-changed")).useAdminConfigChanged;
  createRoot = (await import("react-dom/client")).createRoot;
});

afterAll(() => {
  const g = globalThis as unknown as Record<string, unknown>;
  for (const key of GLOBALS) {
    Object.defineProperty(g, key, { value: saved[key], configurable: true, writable: true });
  }
  delete g.IS_REACT_ACT_ENVIRONMENT;
});

describe("页面监听 admin-config-changed", () => {
  /** 挂一个只调用 useAdminConfigChanged 的组件，返回回调被调用的次数 */
  async function mountListener(target: "model" | "pricing_settings") {
    const calls = { n: 0 };
    const container = dom.window.document.createElement("div");
    dom.window.document.body.appendChild(container);
    function Probe() {
      useAdminConfigChanged(target, () => {
        calls.n++;
      });
      return null;
    }
    let root: Root;
    await act(async () => {
      root = createRoot(container);
      root.render(createElement(Probe));
    });
    return { calls, unmount: () => act(async () => root.unmount()) };
  }

  const fire = (targets: string[]) =>
    act(async () => {
      dom.window.dispatchEvent(
        new dom.window.CustomEvent("admin-config-changed", { detail: { targets } }),
      );
    });

  test("只有这次改动涉及自己关心的对象才重新拉数据", async () => {
    const models = await mountListener("model");
    const pricing = await mountListener("pricing_settings");
    await fire(["model"]);
    expect([models.calls.n, pricing.calls.n]).toEqual([1, 0]);
    await fire(["pricing_settings"]);
    expect([models.calls.n, pricing.calls.n]).toEqual([1, 1]);
    await fire(["model", "pricing_settings"]);
    expect([models.calls.n, pricing.calls.n]).toEqual([2, 2]);
    await models.unmount();
    await pricing.unmount();
  });

  test("页面卸载后不再响应", async () => {
    const models = await mountListener("model");
    await models.unmount();
    await fire(["model"]);
    expect(models.calls.n).toBe(0);
  });
});
