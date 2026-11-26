import { afterAll, beforeAll, describe, expect, spyOn, test } from "bun:test";

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { StudioView as StudioViewType } from "@/pages/drama/studio/view";
import type { Drama } from "@/api/drama/type";
import type { DramaWorkspace } from "@/pages/drama/workspace/use-workspace";

/**
 * 进入片场分节奏挂载：第 0 拍只有分栏骨架（面板在、内容还没挂），之后预览 / 编辑区、再之后资产库 / 片段条逐步出现，
 * 不在同一帧把所有区域都挂完。直接渲染真的片场外壳，靠每个区域外层的 data-phased 标记看谁什么时候挂上。
 */

let dom: DomEnv;
let StudioView: typeof StudioViewType;
let Context: typeof import("@/pages/drama/workspace/context").DramaWorkspaceContext;
beforeAll(async () => {
  dom = await installDom();
  // 当成宽屏：三栏（资产库 / 预览 / 编辑）都显示
  dom.win.matchMedia = (() => ({
    matches: true,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
  })) as never;
  (globalThis as { matchMedia?: unknown }).matchMedia = dom.win.matchMedia;
  // jsdom 没有 DOMRect，分栏库量尺寸时要用
  (globalThis as { DOMRect?: unknown }).DOMRect ??= class {
    x = 0;
    y = 0;
    width = 0;
    height = 0;
    top = 0;
    left = 0;
    right = 0;
    bottom = 0;
  };
  StudioView = (await import("@/pages/drama/studio/view")).StudioView;
  Context = (await import("@/pages/drama/workspace/context")).DramaWorkspaceContext;
}, 60_000);
afterAll(() => dom?.restore());

const drama = {
  id: "d1",
  ratio: "9:16",
  canvasId: "c1",
  videoModel: "",
  imageModels: { character: "", scene: "", prop: "", board: "" },
  promptSettings: { board: { source: "builtin", label: "内置" } },
  episodes: [{ no: 1, title: "初见", locked: true, storyboardStatus: "ready", segmentCount: 0 }],
} as unknown as Drama;

const phased = (c: HTMLElement) =>
  [...c.querySelectorAll("[data-phased]")].map((el) => el.getAttribute("data-phased")).sort();

describe("片场外壳：分拍挂载", () => {
  test("先挂分栏骨架，随后逐帧挂载预览和编辑区、资产库和片段条", async () => {
    const pendingFrames = new Map<number, FrameRequestCallback>();
    let nextId = 0;
    const requestFrame = spyOn(globalThis, "requestAnimationFrame").mockImplementation(
      (callback) => {
        const id = ++nextId;
        pendingFrames.set(id, callback);
        return id;
      },
    );
    const cancelFrame = spyOn(globalThis, "cancelAnimationFrame").mockImplementation((id) => {
      pendingFrames.delete(id);
    });
    const advanceFrame = () =>
      dom.act(async () => {
        const timestamp = performance.now();
        for (const id of [...pendingFrames.keys()]) {
          const callback = pendingFrames.get(id);
          pendingFrames.delete(id);
          callback?.(timestamp);
        }
      });

    try {
      const view = await dom.mount(
        <Context.Provider
          value={{ drama, jobs: [], refresh: async () => {} } as unknown as DramaWorkspace}
        >
          <StudioView drama={drama} no={1} refresh={async () => {}} />
        </Context.Provider>,
      );
      try {
        expect(phased(view.container)).toEqual([]);
        await advanceFrame();
        expect(phased(view.container)).toEqual(["editor", "preview"]);
        await advanceFrame();
        expect(phased(view.container)).toEqual(["clips", "editor", "library", "preview"]);
      } finally {
        await view.unmount();
      }
    } finally {
      cancelFrame.mockRestore();
      requestFrame.mockRestore();
    }
  });
});
