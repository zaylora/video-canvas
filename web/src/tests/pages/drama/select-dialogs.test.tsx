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

import type { Drama, EpisodeSummary } from "@/api/drama/type";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";
import type { ScriptsBody as ScriptsBodyType } from "@/pages/drama/workspace/scripts-dialog";
import type { BreakdownBody as BreakdownBodyType } from "@/pages/drama/workspace/breakdown-dialog";
import type { EpisodeGrid as EpisodeGridType } from "@/pages/drama/workspace/episode-pick";
import type { DramaWorkspaceContext as DramaWorkspaceContextType } from "@/pages/drama/workspace/context";
import type { Dialog as DialogType } from "@/components/ui/dialog";

/**
 * 选集弹窗：写剧本（只列已规划的集、按范围选、衔接提示）与资产拆解（没有比例选择、按范围选）。
 */

let dom: DomEnv;
let ScriptsBody: typeof ScriptsBodyType;
let BreakdownBody: typeof BreakdownBodyType;
let EpisodeGrid: typeof EpisodeGridType;
let DramaWorkspaceContext: typeof DramaWorkspaceContextType;
let Dialog: typeof DialogType;
beforeAll(async () => {
  dom = await installDom();
  ScriptsBody = (await import("@/pages/drama/workspace/scripts-dialog")).ScriptsBody;
  BreakdownBody = (await import("@/pages/drama/workspace/breakdown-dialog")).BreakdownBody;
  EpisodeGrid = (await import("@/pages/drama/workspace/episode-pick")).EpisodeGrid;
  DramaWorkspaceContext = (await import("@/pages/drama/workspace/context")).DramaWorkspaceContext;
  Dialog = (await import("@/components/ui/dialog")).Dialog;
});
afterAll(() => dom.restore());

const ep = (no: number, patch: Partial<EpisodeSummary> = {}) =>
  ({
    no,
    title: `第${no}集`,
    scriptStatus: "ready",
    locked: false,
    revision: 1,
    ...patch,
  }) as EpisodeSummary;

/** 1 有剧本，2–4 没有，5 有剧本，6 已拆解，7 还没规划（没标题） */
const dramaWith = (patch: Partial<Drama> = {}) =>
  ({
    id: "d1",
    ratio: "9:16",
    episodes: [
      ep(1),
      ep(2, { scriptStatus: "empty" }),
      ep(3, { scriptStatus: "empty" }),
      ep(4, { scriptStatus: "empty" }),
      ep(5),
      ep(6, { locked: true }),
      ep(7, { title: "", scriptStatus: "empty" }),
    ],
    ...patch,
  }) as unknown as Drama;

let post: ReturnType<typeof spyOn>;
let gapReply: number[] | null = null;
beforeEach(() => {
  gapReply = null;
  post = spyOn(service, "post").mockImplementation((async (url: string, body: unknown) => {
    if (url.endsWith("/estimate")) {
      const episodes = (body as { episodes: number[] }).episodes;
      return { credits: 22 * episodes.length, gap_episodes: gapReply ?? [] };
    }
    return {
      id: "j9",
      step: (body as { step: string }).step,
      episodes: (body as { episodes: number[] }).episodes,
      status: "queued",
      progress: 0,
      credits: 0,
      error_code: 0,
      error_message: "",
      created_at: "2026-11-04T10:00:00Z",
      finished_at: null,
      lane: "user",
    };
  }) as never);
});
afterEach(() => post.mockRestore());

const mountWith = (drama: Drama, node: React.ReactElement) =>
  dom.mount(
    <DramaWorkspaceContext.Provider value={{ drama, jobs: [], refresh: async () => {} } as never}>
      <Dialog open>{node}</Dialog>
    </DramaWorkspaceContext.Provider>,
  );
const cell = (c: HTMLElement, no: number) =>
  [...c.querySelectorAll('[data-slot="episode-grid"] button')].find((b) =>
    (b.textContent ?? "").startsWith(`${no}.`),
  ) as HTMLButtonElement;
const picked = (c: HTMLElement) =>
  [...c.querySelectorAll('[data-slot="episode-grid"] button[aria-pressed="true"]')].map((b) =>
    Number((b.textContent ?? "").match(/^(\d+)\./)?.[1]),
  );
const byText = (c: HTMLElement, text: string) =>
  [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);
const setRange = async (c: HTMLElement, a: string, b: string) => {
  await dom.type(c.querySelector('input[aria-label="起始集"]'), a);
  await dom.type(c.querySelector('input[aria-label="结束集"]'), b);
  await dom.click(byText(c, "选中"));
};

describe("生成分集剧本弹窗", () => {
  test("只列已规划的集（没有标题的第 7 集不列）；默认选还没有剧本的；已拆解的置灰", async () => {
    const m = await mountWith(dramaWith(), <ScriptsBody onClose={() => {}} />);
    expect(cell(m.container, 7)).toBeUndefined();
    expect(picked(m.container)).toEqual([2, 3, 4]);
    expect(cell(m.container, 6).disabled).toBe(true);
    expect(cell(m.container, 6).title).toBe("已拆解");
    expect(cell(m.container, 5).textContent).toContain("已有剧本");
    await m.unmount();
  });

  test("按范围选：第 a–b 集，已拆解的跳过；a > b 自动对调", async () => {
    const m = await mountWith(dramaWith(), <ScriptsBody onClose={() => {}} />);
    await setRange(m.container, "1", "6");
    expect(picked(m.container)).toEqual([1, 2, 3, 4, 5]);
    expect(m.container.textContent).toContain("其中 2 集会覆盖现有剧本");
    await setRange(m.container, "4", "3");
    expect(picked(m.container)).toEqual([3, 4]);
    await m.unmount();
  });

  test("衔接提示：选中集的上一集没剧本、也不在选择里，说明按梗概和结尾悬念衔接", async () => {
    const m = await mountWith(dramaWith(), <ScriptsBody onClose={() => {}} />);
    const note = () => m.container.querySelector('[role="note"]')?.textContent ?? null;
    await dom.click(byText(m.container, "清空"));
    expect(note()).toBeNull();
    await dom.click(cell(m.container, 4));
    expect(note()).toBe("第 3 集还没有剧本，第 4 集会按它的梗概和结尾悬念衔接");
    // 上一集（3）也在本次选择里：第 4 集不再提示，但第 3 集的上一集（2）还是缺的
    await dom.click(cell(m.container, 3));
    expect(note()).toBe("第 2 集还没有剧本，第 3 集会按它的梗概和结尾悬念衔接");
    // 第 2 集的上一集有剧本：整条链都接上了
    await dom.click(cell(m.container, 2));
    expect(note()).toBeNull();
    await m.unmount();
  });

  test("估价接口返回的衔接缺口优先：后端说没有缺口就不提示", async () => {
    gapReply = [];
    const m = await mountWith(dramaWith(), <ScriptsBody onClose={() => {}} />);
    await dom.click(byText(m.container, "清空"));
    await dom.click(cell(m.container, 4));
    await dom.flush(400);
    expect(post.mock.calls.at(-1)?.[1]).toMatchObject({ step: "scripts", episodes: [4] });
    expect(m.container.querySelector('[role="note"]') === null).toBe(true);
    await m.unmount();
  });

  test("从某一集的空状态进来：预先选中那一集", async () => {
    const m = await mountWith(dramaWith(), <ScriptsBody initial={[3]} onClose={() => {}} />);
    expect(picked(m.container)).toEqual([3]);
    await m.unmount();
  });

  test("提交：只带选中的集", async () => {
    let closed = 0;
    const m = await mountWith(dramaWith(), <ScriptsBody onClose={() => (closed += 1)} />);
    await dom.click(byText(m.container, "生成 3 集剧本"));
    await dom.flush();
    const call = post.mock.calls.find((c) => c[0] === "/dramas/d1/jobs");
    expect(call?.[1]).toEqual({ step: "scripts", episodes: [2, 3, 4] });
    expect(closed).toBe(1);
    await m.unmount();
  });
});

describe("资产拆解弹窗：没有比例选择", () => {
  test("老项目没定比例也不再要求选比例，页面里没有任何比例按钮", async () => {
    const m = await mountWith(
      dramaWith({ ratio: "" }),
      <BreakdownBody onClose={() => {}} onStarted={() => {}} />,
    );
    const text = m.container.textContent ?? "";
    expect(text).not.toContain("成片比例");
    expect(text).not.toContain("必选");
    expect(byText(m.container, "9:16")).toBeUndefined();
    await m.unmount();
  });

  test("默认选第一个可用集；提交的任务请求里没有 ratio", async () => {
    let started = 0;
    const m = await mountWith(
      dramaWith(),
      <BreakdownBody onClose={() => {}} onStarted={() => (started += 1)} />,
    );
    expect(picked(m.container)).toEqual([1]);
    await dom.click(byText(m.container, "开始拆解"));
    await dom.flush();
    const call = post.mock.calls.find((c) => c[0] === "/dramas/d1/jobs");
    expect(call?.[1]).toEqual({ step: "breakdown", episodes: [1] });
    expect(started).toBe(1);
    await m.unmount();
  });

  test("按范围选只选有剧本、没锁定的；没有剧本的和已拆解的跳过", async () => {
    const m = await mountWith(
      dramaWith(),
      <BreakdownBody onClose={() => {}} onStarted={() => {}} />,
    );
    await setRange(m.container, "1", "7");
    expect(picked(m.container)).toEqual([1, 5]);
    expect(cell(m.container, 2).disabled).toBe(true);
    expect(cell(m.container, 2).title).toBe("还没有剧本");
    expect(cell(m.container, 6).disabled).toBe(true);
    await m.unmount();
  });
});

describe("选集网格：集多时虚拟化", () => {
  test("三千集只渲染可见的几行", async () => {
    const { renderToStaticMarkup } = await import("react-dom/server");
    const options = Array.from({ length: 3000 }, (_, i) => ({
      no: i + 1,
      title: `第${i + 1}集`,
      disabled: false,
      reason: null,
    }));
    const out = renderToStaticMarkup(
      <EpisodeGrid options={options} selected={[]} onToggle={() => {}} />,
    );
    const rendered = [...out.matchAll(/aria-pressed/g)].length;
    expect(rendered).toBeGreaterThan(10);
    expect(rendered).toBeLessThan(80);
  });
});
