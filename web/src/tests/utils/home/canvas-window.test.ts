import { afterEach, describe, expect, test } from "bun:test";

import { openCanvasWindow } from "@/utils/home/canvas-window";

type FakeWindow = { opener: unknown; closed: boolean; location: { href: string }; close(): void };

const makeWindow = (): FakeWindow => ({
  opener: "origin",
  closed: false,
  location: { href: "about:blank" },
  close() {
    this.closed = true;
  },
});

/** 把 window.open 换成假的，记下调用参数 */
function stubOpen(result: FakeWindow | null) {
  const calls: unknown[][] = [];
  (globalThis as { window?: unknown }).window = {
    open: (...args: unknown[]) => {
      calls.push(args);
      return result;
    },
  };
  return calls;
}

afterEach(() => {
  delete (globalThis as { window?: unknown }).window;
});

describe("openCanvasWindow：新建画布要在新窗口打开，但创建是异步的", () => {
  test("先同步开一个空白窗口，创建完成后再把它带去画布；新窗口摘掉 opener", () => {
    const win = makeWindow();
    const calls = stubOpen(win);
    const handle = openCanvasWindow();
    expect(calls).toEqual([["", "_blank"]]);
    expect(win.opener).toBeNull();
    expect(handle.go("/canvas/ab12")).toBe(true);
    expect(win.location.href).toBe("/canvas/ab12");
  });

  test("创建失败时关掉空白窗口，不留一个空标签页", () => {
    const win = makeWindow();
    stubOpen(win);
    openCanvasWindow().cancel();
    expect(win.closed).toBe(true);
  });

  test("窗口被浏览器拦截（open 返回 null）：go 返回 false，由调用方在当前页跳转", () => {
    stubOpen(null);
    const handle = openCanvasWindow();
    expect(handle.go("/canvas/ab12")).toBe(false);
    expect(() => handle.cancel()).not.toThrow();
  });
});
