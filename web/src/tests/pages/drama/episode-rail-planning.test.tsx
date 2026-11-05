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
import { renderToStaticMarkup } from "react-dom/server";

import type { Drama, EpisodeSummary } from "@/api/drama/type";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import { DramaWorkspaceContext } from "@/pages/drama/workspace/context";
import { EpisodeRail, canContinuePlanning } from "@/pages/drama/workspace/episode-rail";
import service from "@/utils/requests/service";
import type { ContinueBody as ContinueBodyType } from "@/pages/drama/workspace/continue-plan-dialog";
import type { Dialog as DialogType } from "@/components/ui/dialog";

/**
 * 分集栏的长篇适配：虚拟列表、规划中禁用新增、末尾「继续规划」及其确认框。
 */

const episodes = (n: number): EpisodeSummary[] =>
  Array.from(
    { length: n },
    (_, i) => ({ no: i + 1, title: `第${i + 1}集`, locked: false }) as EpisodeSummary,
  );

const drama = (patch: Partial<Drama> = {}, count = 3) =>
  ({
    id: "d1",
    phase: "done",
    sourceKind: "novel",
    unitsTotal: 1215,
    rangeEnd: 120,
    unitKind: "chapter",
    sourceChars: 4860000,
    episodes: episodes(count),
    ...patch,
  }) as unknown as Drama;

const html = (d: Drama, stage: "script" | "assets" = "script") =>
  renderToStaticMarkup(
    <DramaWorkspaceContext.Provider
      value={{ drama: d, jobs: [], refresh: async () => {} } as never}
    >
      <EpisodeRail stage={stage} ep={1} onPick={() => {}} onDoc={() => {}} />
    </DramaWorkspaceContext.Provider>,
  );

describe("分集栏：虚拟列表", () => {
  test("几千集也只渲染可见的几十行，不是全量", () => {
    const out = html(drama({}, 3000));
    const rendered = [...out.matchAll(/aria-label="第 \d+ 集 · /g)].length;
    expect(rendered).toBeGreaterThan(5);
    expect(rendered).toBeLessThan(60);
    expect(out).toContain('aria-label="第 1 集 · 第1集"');
    expect(out).not.toContain('aria-label="第 3000 集');
  });

  test("顶上的「大纲 / 人物 / 全集」固定在列表外", () => {
    const out = html(drama());
    expect(out.indexOf('aria-label="故事大纲"')).toBeLessThan(out.indexOf('aria-label="全集"'));
    expect(out.indexOf('aria-label="全集"')).toBeLessThan(out.indexOf('aria-label="第 1 集'));
  });
});

describe("分集栏：规划中与继续规划", () => {
  test("规划中：「新增一集」禁用并写明原因", () => {
    const out = html(drama({ phase: "running" }));
    const button = out.slice(out.lastIndexOf("<button", out.indexOf('aria-label="新增一集"')));
    expect(button).toMatch(/^<button[^>]*disabled=""/);
    expect(button).toContain('title="正在规划，完成或暂停后再新增"');
  });

  test("暂停或完成后「新增一集」恢复可点", () => {
    for (const phase of ["paused", "done"] as const) {
      const out = html(drama({ phase }));
      const button = out.slice(out.lastIndexOf("<button", out.indexOf('aria-label="新增一集"')));
      expect(button).not.toMatch(/^<button[^>]*disabled=""/);
    }
  });

  test("规划范围完成且有剩余：末尾出现「继续规划」；规划中 / 已规划完 / 剧本型都没有", () => {
    expect(html(drama())).toContain('aria-label="继续规划"');
    expect(html(drama({ phase: "running" }))).not.toContain("继续规划");
    expect(html(drama({ rangeEnd: 1215 }))).not.toContain("继续规划");
    expect(html(drama({ sourceKind: "script" }))).not.toContain("继续规划");
    expect(
      canContinuePlanning({ phase: "paused", sourceKind: "novel", unitsTotal: 9, rangeEnd: 3 }),
    ).toBe(false);
    expect(canContinuePlanning({ phase: "done", sourceKind: "", unitsTotal: 0, rangeEnd: 0 })).toBe(
      false,
    );
  });
});

describe("「继续规划」确认框", () => {
  let dom: DomEnv;
  let ContinueBody: typeof ContinueBodyType;
  let Dialog: typeof DialogType;
  let post: ReturnType<typeof spyOn>;
  let refreshed = 0;
  let closed = 0;

  beforeAll(async () => {
    dom = await installDom();
    ContinueBody = (await import("@/pages/drama/workspace/continue-plan-dialog")).ContinueBody;
    Dialog = (await import("@/components/ui/dialog")).Dialog;
  });
  afterAll(() => dom.restore());
  beforeEach(() => {
    refreshed = 0;
    closed = 0;
    post = spyOn(service, "post").mockImplementation((async (url: string) => {
      if (url.endsWith("/planning/estimate")) {
        return {
          episodes_min: 100,
          episodes_max: 160,
          episodes_exact: false,
          credits: 480,
          minutes: 12,
        };
      }
      return {};
    }) as never);
  });
  afterEach(() => post.mockRestore());

  const mountBody = () =>
    dom.mount(
      <DramaWorkspaceContext.Provider
        value={
          {
            drama: drama(),
            jobs: [],
            refresh: async () => {
              refreshed += 1;
            },
          } as never
        }
      >
        <Dialog open>
          <ContinueBody onClose={() => (closed += 1)} />
        </Dialog>
      </DramaWorkspaceContext.Provider>,
    );
  const byText = (c: HTMLElement, text: string) =>
    [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);

  test("默认「全部剩余」：显示剩余范围、预计集数、积分、耗时；确认后 range_end = 0 并刷新、关闭", async () => {
    const m = await mountBody();
    await dom.flush(400);
    const text = m.container.textContent ?? "";
    expect(text).toContain("第 121–1,215 章");
    expect(text).toContain("预计约 100–160 集");
    expect(text).toContain("480");
    expect(text).toContain("约 12 分钟");
    expect(post.mock.calls[0][0]).toBe("/dramas/d1/planning/estimate");
    expect(post.mock.calls[0][1]).toEqual({ range_end: 0 });
    await dom.click(byText(m.container, "开始规划"));
    await dom.flush();
    const rangeCall = post.mock.calls.find((c) => c[0] === "/dramas/d1/planning/range");
    expect(rangeCall?.[1]).toEqual({ range_end: 0 });
    expect(refreshed).toBe(1);
    expect(closed).toBe(1);
    await m.unmount();
  });

  test("切到「规划到第 N 章」：输入 N，预估和提交都带这个终点", async () => {
    const m = await mountBody();
    await dom.click([...m.container.querySelectorAll('[role="tab"]')][1]);
    await dom.type(m.container.querySelector("input"), "300");
    await dom.flush(400);
    expect(m.container.textContent).toContain("第 121–300 章");
    expect(post.mock.calls.at(-1)?.[1]).toEqual({ range_end: 300 });
    await dom.click(byText(m.container, "开始规划"));
    await dom.flush();
    expect(post.mock.calls.find((c) => c[0] === "/dramas/d1/planning/range")?.[1]).toEqual({
      range_end: 300,
    });
    await m.unmount();
  });

  test("范围非法（状态不对 67019）：错误就地显示，框不关", async () => {
    const { ApiError } = await import("@/utils/requests/request");
    post.mockImplementation((async (url: string) => {
      if (url.endsWith("/range")) throw new ApiError("", 67019, 409);
      return { episodes_min: 1, episodes_max: 2, episodes_exact: false, credits: 1, minutes: 1 };
    }) as never);
    const m = await mountBody();
    await dom.click(byText(m.container, "开始规划"));
    await dom.flush();
    expect(m.container.querySelector('[role="alert"]')?.textContent).toContain("当前阶段");
    expect(closed).toBe(0);
    await m.unmount();
  });
});
