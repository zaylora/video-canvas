import { afterAll, beforeAll, describe, expect, mock, test } from "bun:test";

import type { EpisodeSummary } from "@/api/drama/type";
import type { StudioArea as StudioAreaType } from "@/pages/drama/workspace/studio-area";
import type { DramaWorkspace } from "@/pages/drama/workspace/use-workspace";
import { installDom, type DomEnv } from "@/tests/helpers/dom";

/**
 * 片场的「还没拆解」引导：「去拆解」直接打开拆解弹窗（和总览、剧本、资产管理一致），
 * 不是跳到剧本页；剧本还没写好时才去剧本页。
 */

mock.module("@/hooks/use-drama-video-model", () => ({
  useDramaVideoModel: () => ({
    models: [],
    status: "ready",
    model: undefined,
    setModel: () => {},
    maxSeconds: 15,
  }),
}));

let dom: DomEnv;
let StudioArea: typeof StudioAreaType;
let Context: typeof import("@/pages/drama/workspace/context").DramaWorkspaceContext;
beforeAll(async () => {
  dom = await installDom();
  StudioArea = (await import("@/pages/drama/workspace/studio-area")).StudioArea;
  Context = (await import("@/pages/drama/workspace/context")).DramaWorkspaceContext;
  // 片场模块连带 Tiptap 编辑器等，首次导入较慢
}, 60_000);
afterAll(() => dom?.restore());

const episode = (patch: Partial<EpisodeSummary>) =>
  ({
    no: 1,
    title: "初见",
    locked: false,
    scriptStatus: "ready",
    storyboardStatus: "empty",
    ...patch,
  }) as EpisodeSummary;

async function renderGate(ep: EpisodeSummary) {
  const went: Array<[string, number | undefined]> = [];
  let opened = 0;
  const view = await dom.mount(
    <Context.Provider
      value={
        {
          drama: { id: "d1", videoModel: "", episodes: [ep] },
          jobs: [],
          refresh: async () => {},
        } as unknown as DramaWorkspace
      }
    >
      <StudioArea
        ep={1}
        onGo={(stage, no) => went.push([stage, no])}
        onBreakdownOpen={() => (opened += 1)}
      />
    </Context.Provider>,
  );
  const button = [...view.container.querySelectorAll("button")].find((b) =>
    /去拆解|去写剧本/.test(b.textContent ?? ""),
  );
  await dom.click(button);
  await view.unmount();
  return { went, opened, label: button?.textContent };
}

describe("片场：还没拆解资产时的引导", () => {
  test("剧本已写好：「去拆解」打开拆解弹窗，不跳页", async () => {
    const { went, opened, label } = await renderGate(episode({ scriptStatus: "ready" }));
    expect(label).toContain("去拆解");
    expect(opened).toBe(1);
    expect(went).toEqual([]);
  });

  test("剧本还没写好：「去写剧本」跳到这一集的剧本页", async () => {
    const { went, opened, label } = await renderGate(episode({ scriptStatus: "empty" }));
    expect(label).toContain("去写剧本");
    expect(opened).toBe(0);
    expect(went).toEqual([["script", 1]]);
  });
});
