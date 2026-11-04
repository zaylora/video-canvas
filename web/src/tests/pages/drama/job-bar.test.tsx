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

import type { Drama, DramaJob } from "@/api/drama/type";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";
import type { JobBar as JobBarType } from "@/pages/drama/workspace/job-bar";
import type { DramaWorkspaceContext as DramaWorkspaceContextType } from "@/pages/drama/workspace/context";

/**
 * 工作台任务条：自动规划那一行（研读 / 正在规划第 a–b 章 / 暂停 / 失败 / 改用内置重试）与用户任务各占一行。
 */

let dom: DomEnv;
let JobBar: typeof JobBarType;
let DramaWorkspaceContext: typeof DramaWorkspaceContextType;
beforeAll(async () => {
  dom = await installDom();
  JobBar = (await import("@/pages/drama/workspace/job-bar")).JobBar;
  DramaWorkspaceContext = (await import("@/pages/drama/workspace/context")).DramaWorkspaceContext;
});
afterAll(() => dom.restore());

const job = (patch: Partial<DramaJob> = {}): DramaJob => ({
  id: "j1",
  step: "plan",
  episodes: [],
  status: "running",
  progress: 10,
  credits: 0,
  errorCode: 0,
  errorMessage: "",
  createdAt: "2026-11-04T10:00:00Z",
  finishedAt: null,
  result: null,
  lane: "pipeline",
  batchFrom: 301,
  batchTo: 312,
  ...patch,
});

const drama = (patch: Partial<Drama> = {}) =>
  ({
    id: "d1",
    phase: "running",
    planCursor: 300,
    rangeEnd: 1200,
    pauseReason: "",
    pauseCode: "",
    unitKind: "chapter",
    createdAt: new Date(Date.now() - 3_600_000).toISOString(),
    ratio: "9:16",
    episodes: Array.from({ length: 52 }, (_, i) => ({ no: i + 1 })),
    ...patch,
  }) as unknown as Drama;

let post: ReturnType<typeof spyOn>;
let refreshed = 0;
beforeEach(() => {
  post = spyOn(service, "post").mockResolvedValue({} as never);
  refreshed = 0;
});
afterEach(() => post.mockRestore());

const mountBar = (d: Drama, jobs: DramaJob[]) =>
  dom.mount(
    <DramaWorkspaceContext.Provider
      value={
        {
          drama: d,
          jobs,
          refresh: async () => {
            refreshed += 1;
          },
        } as never
      }
    >
      <JobBar inPlace={{ analyzingView: false, storyboardEp: null }} />
    </DramaWorkspaceContext.Provider>,
  );
const byText = (c: HTMLElement, text: string) =>
  [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);

describe("JobBar：自动规划行", () => {
  test("规划中：正在规划第 301–312 章 · 已出 52 集目录，带进度和「暂停」", async () => {
    const m = await mountBar(drama(), [job()]);
    const row = m.container.querySelector('[data-slot="planning-row"]') as HTMLElement;
    expect(row.textContent).toContain("正在规划第 301–312 章");
    expect(row.textContent).toContain("已出 52 集目录");
    expect(row.textContent).toContain("25%");
    expect(row.textContent).toContain("还需约");
    await dom.click(byText(row, "暂停"));
    await dom.flush();
    expect(post.mock.calls[0][0]).toBe("/dramas/d1/planning/pause");
    expect(refreshed).toBe(1);
    await m.unmount();
  });

  test("研读中：正在粗读全书", async () => {
    const m = await mountBar(drama({ planCursor: 0 }), [
      job({ step: "analyze", batchFrom: null, batchTo: null, progress: 40 }),
    ]);
    const row = m.container.querySelector('[data-slot="planning-row"]') as HTMLElement;
    expect(row.textContent).toContain("正在粗读全书");
    expect(row.textContent).toContain("40%");
    await m.unmount();
  });

  test("用户暂停：已规划到第 N 章，点「继续」调 resume", async () => {
    const m = await mountBar(drama({ phase: "paused", pauseCode: "user", planCursor: 44 }), []);
    const row = m.container.querySelector('[data-slot="planning-row"]') as HTMLElement;
    expect(row.textContent).toContain("分集规划已暂停");
    expect(row.textContent).toContain("已规划到第 44 章");
    await dom.click(byText(row, "继续"));
    await dom.flush();
    expect(post.mock.calls[0][0]).toBe("/dramas/d1/planning/resume");
    await m.unmount();
  });

  test("积分不足：写原因，给「继续」；失败：给「重试」，都走 resume", async () => {
    const credits = await mountBar(drama({ phase: "paused", pauseCode: "credits" }), []);
    expect(credits.container.textContent).toContain("积分不足，充值后继续");
    expect(byText(credits.container, "继续")).toBeTruthy();
    await credits.unmount();
    const failed = await mountBar(
      drama({ phase: "paused", pauseCode: "failed", pauseReason: "模型超时" }),
      [],
    );
    expect(failed.container.textContent).toContain("模型超时");
    await dom.click(byText(failed.container, "重试"));
    await dom.flush();
    expect(post.mock.calls[0][0]).toBe("/dramas/d1/planning/resume");
    await failed.unmount();
  });

  test("自定义提示词导致失败：多一个「改用内置重试」，把剧本改编改回内置后继续", async () => {
    const m = await mountBar(drama({ phase: "paused", pauseCode: "custom_prompt" }), []);
    expect(byText(m.container, "重试")).toBeTruthy();
    await dom.click(byText(m.container, "改用内置重试"));
    await dom.flush();
    expect(post.mock.calls[0][0]).toBe("/dramas/d1/planning/retry-builtin");
    expect(post.mock.calls[0][1]).toEqual({ step: "adapt" });
    await m.unmount();
  });

  test("规划完成（done）没有规划行；规划任务失败卡片不会当用户任务再显示一遍", async () => {
    const m = await mountBar(drama({ phase: "done" }), [
      job({ status: "failed", errorCode: 67015 }),
    ]);
    expect(m.container.querySelector('[data-slot="planning-row"]')).toBeNull();
    expect(m.container.textContent).toBe("");
    await m.unmount();
  });

  test("规划与用户任务同时进行：各占一行，互不影响", async () => {
    const m = await mountBar(drama(), [
      job(),
      job({
        id: "u1",
        step: "scripts",
        lane: "user",
        episodes: [1, 2, 3],
        batchFrom: null,
        batchTo: null,
        progress: 50,
      }),
    ]);
    const rows = [...m.container.querySelectorAll('[role="status"]')];
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain("正在规划第 301–312 章");
    expect(rows[1].textContent).toContain("分集剧本");
    await m.unmount();
  });
});
