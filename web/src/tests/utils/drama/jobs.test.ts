import { describe, expect, test } from "bun:test";

import type { DramaJob } from "@/api/drama/type";
import {
  activeStoryboardJob,
  busyReason,
  bannerJobs,
  coveredInPlace,
  jobRunningText,
  jobStage,
  isActiveJob,
  isPipelineJob,
  jobConflictReason,
  userJobs,
  planningRow,
  planningEtaMinutes,
  jobTitle,
  mergeJob,
  pruneFinished,
  reconcileJobs,
} from "@/utils/drama/jobs";

const job = (patch: Partial<DramaJob> = {}): DramaJob => ({
  id: "j1",
  step: "breakdown",
  episodes: [1],
  status: "running",
  progress: 20,
  credits: 0,
  errorCode: 0,
  errorMessage: "",
  createdAt: "2026-10-28T10:00:00Z",
  finishedAt: null,
  lane: "user",
  batchFrom: null,
  batchTo: null,
  ...patch,
});

describe("任务合并 mergeJob", () => {
  test("新任务插入，按创建时间新的在前", () => {
    const merged = mergeJob(
      [job({ id: "old", createdAt: "2026-10-28T09:00:00Z" })],
      job({ id: "new", createdAt: "2026-10-28T10:00:00Z" }),
    );
    expect(merged.map((j) => j.id)).toEqual(["new", "old"]);
  });

  test("同 id 更新状态与进度", () => {
    const merged = mergeJob([job({ progress: 20 })], job({ progress: 60 }));
    expect(merged).toHaveLength(1);
    expect(merged[0].progress).toBe(60);
  });

  test("进度不回退：乱序到达的旧进度被忽略", () => {
    const merged = mergeJob([job({ progress: 60 })], job({ progress: 30 }));
    expect(merged[0].progress).toBe(60);
  });

  test("终态不被迟到的非终态事件覆盖", () => {
    const done = job({ status: "succeeded", progress: 100, finishedAt: "2026-10-28T10:05:00Z" });
    expect(mergeJob([done], job({ status: "running", progress: 80 }))[0].status).toBe("succeeded");
    expect(mergeJob([done], job({ status: "queued", progress: 0 }))[0].status).toBe("succeeded");
  });

  test("running 不会退回 queued", () => {
    expect(
      mergeJob([job({ status: "running" })], job({ status: "queued", progress: 0 }))[0].status,
    ).toBe("running");
  });

  test("成功收尾：进度补到 100 并带上扣费", () => {
    const merged = mergeJob(
      [job({ progress: 60 })],
      job({ status: "succeeded", progress: 100, credits: 12, finishedAt: "x" }),
    );
    expect(merged[0]).toMatchObject({ status: "succeeded", progress: 100, credits: 12 });
  });
});

describe("挂载 / 重连对账 reconcileJobs", () => {
  test("服务端的进行中任务并入本地", () => {
    const { jobs, ended } = reconcileJobs([], [job({ id: "a" })]);
    expect(jobs.map((j) => j.id)).toEqual(["a"]);
    expect(ended).toEqual([]);
  });

  test("本地还显示进行中、服务端已经没有：算已结束，移出并提示重新拉详情", () => {
    const { jobs, ended } = reconcileJobs([job({ id: "a" }), job({ id: "b" })], [job({ id: "b" })]);
    expect(jobs.map((j) => j.id)).toEqual(["b"]);
    expect(ended.map((j) => j.id)).toEqual(["a"]);
  });

  test("本地已经收到终态的任务（失败卡片）保留，不当成结束", () => {
    const failed = job({ id: "f", status: "failed", errorCode: 67008 });
    const { jobs, ended } = reconcileJobs([failed], []);
    expect(jobs).toEqual([failed]);
    expect(ended).toEqual([]);
  });

  test("服务端的新进度覆盖本地旧进度", () => {
    const { jobs } = reconcileJobs([job({ progress: 10 })], [job({ progress: 50 })]);
    expect(jobs[0].progress).toBe(50);
  });
});

describe("展示与禁用原因", () => {
  test("任务标题", () => {
    expect(jobTitle(job({ step: "analyze", episodes: [] }))).toBe("研读剧本");
    expect(jobTitle(job({ step: "outline", episodes: [] }))).toBe("故事大纲与人物");
    expect(jobTitle(job({ step: "scripts", episodes: [3, 4, 5] }))).toBe("分集剧本 · 3 集");
    expect(jobTitle(job({ step: "breakdown", episodes: [2] }))).toBe("资产拆解 · 第 2 集");
    expect(jobTitle(job({ step: "breakdown", episodes: [1, 2, 3] }))).toBe("资产拆解 · 3 集");
    expect(jobTitle(job({ step: "asset_prompts", episodes: [] }))).toBe("写资产提示词");
  });

  test("有进行中任务时给禁用原因，否则为 null", () => {
    expect(busyReason([])).toBeNull();
    expect(busyReason([job({ status: "succeeded" })])).toBeNull();
    expect(busyReason([job({ status: "failed" })])).toBeNull();
    expect(busyReason([job({ status: "running", step: "scripts", episodes: [2, 3] })])).toContain(
      "分集剧本",
    );
    expect(busyReason([job({ status: "queued" })])).not.toBeNull();
  });

  test("jobConflictReason 与后端同规则：分镜/合成只和同一集的分镜/合成冲突", () => {
    const sb3 = job({ step: "storyboard", episodes: [3] });
    expect(jobConflictReason([sb3], "storyboard", [4])).toBeNull();
    expect(jobConflictReason([sb3], "storyboard", [3])).toContain("第 3 集");
    expect(jobConflictReason([sb3], "compose", [3])).toContain("分镜脚本");
    expect(jobConflictReason([sb3], "compose", [5])).toBeNull();
    expect(
      jobConflictReason(
        [job({ step: "storyboard", episodes: [3], status: "succeeded" })],
        "storyboard",
        [3],
      ),
    ).toBeNull();
    // 文本任务不挡分镜，分镜也不挡文本任务
    expect(
      jobConflictReason([job({ step: "scripts", episodes: [3] })], "storyboard", [3]),
    ).toBeNull();
    expect(jobConflictReason([sb3], "breakdown", [3])).toBeNull();
    // 文本任务之间互斥，不看集号
    expect(
      jobConflictReason([job({ step: "scripts", episodes: [1] })], "breakdown", [9]),
    ).toContain("分集剧本");
    // 自动规划不挡用户任务
    expect(
      jobConflictReason([job({ step: "plan", lane: "pipeline", episodes: [] })], "storyboard", [3]),
    ).toBeNull();
    // 没写集号的非文本任务按全项目算
    expect(
      jobConflictReason([job({ step: "compose", episodes: [] })], "storyboard", [3]),
    ).not.toBeNull();
  });

  test("isActiveJob", () => {
    expect(isActiveJob(job({ status: "queued" }))).toBe(true);
    expect(isActiveJob(job({ status: "running" }))).toBe(true);
    expect(isActiveJob(job({ status: "canceled" }))).toBe(false);
  });

  test("只保留最近几条已结束的任务，进行中的全留", () => {
    const jobs = [
      job({ id: "r", status: "running", createdAt: "2026-10-28T10:10:00Z" }),
      ...[1, 2, 3, 4].map((n) =>
        job({ id: `d${n}`, status: "succeeded", createdAt: `2026-10-28T10:0${n}:00Z` }),
      ),
    ];
    const pruned = pruneFinished(jobs, 2);
    expect(pruned.map((j) => j.id)).toEqual(["r", "d4", "d3"]);
  });
});

describe("页面里就地显示进度的任务，顶部进度条不再重复", () => {
  const view = { analyzingView: false, storyboardEp: null };

  test("正在看第 1 集片场：这一集的分镜任务也进顶部，和看别的集时一致", () => {
    const sb = job({ step: "storyboard", episodes: [1] });
    expect(coveredInPlace(sb, { ...view, storyboardEp: 1 })).toBe(false);
  });

  test("看的是别的集或不在片场：照常进顶部", () => {
    const sb = job({ step: "storyboard", episodes: [1] });
    expect(coveredInPlace(sb, { ...view, storyboardEp: 2 })).toBe(false);
    expect(coveredInPlace(sb, view)).toBe(false);
  });

  test("分镜失败了不算就地显示：顶部照样给出原因和重试", () => {
    const failed = job({ step: "storyboard", episodes: [1], status: "failed" });
    expect(coveredInPlace(failed, { ...view, storyboardEp: 1 })).toBe(false);
  });

  test("研读视图开着时，研读和大纲任务不进顶部；其他任务照常", () => {
    const on = { ...view, analyzingView: true };
    expect(coveredInPlace(job({ step: "analyze", episodes: [] }), on)).toBe(true);
    expect(coveredInPlace(job({ step: "outline", episodes: [] }), on)).toBe(true);
    expect(coveredInPlace(job({ step: "breakdown" }), on)).toBe(false);
  });

  test("activeStoryboardJob：找出正在给这一集生成分镜的任务", () => {
    const sb = job({ id: "s", step: "storyboard", episodes: [2] });
    const jobs = [job({ id: "b" }), sb, job({ id: "d", step: "storyboard", status: "succeeded" })];
    expect(activeStoryboardJob(jobs, 2)?.id).toBe("s");
    expect(activeStoryboardJob(jobs, 1)).toBeUndefined();
  });
});

describe("页内进度条 bannerJobs", () => {
  test("进行中的都显示", () => {
    const jobs = [job({ id: "a", status: "running" }), job({ id: "b", status: "queued" })];
    expect(bannerJobs(jobs).map((j) => j.id)).toEqual(["a", "b"]);
  });

  test("最新一条失败了才显示它；已有更新的成功任务就不显示旧失败", () => {
    const failedNewest = [
      job({ id: "n", status: "failed", createdAt: "2026-10-28T11:00:00Z" }),
      job({ id: "o", status: "succeeded", createdAt: "2026-10-28T10:00:00Z" }),
    ];
    expect(bannerJobs(failedNewest).map((j) => j.id)).toEqual(["n"]);
    const succeededNewest = [
      job({ id: "n", status: "succeeded", createdAt: "2026-10-28T11:00:00Z" }),
      job({ id: "o", status: "failed", createdAt: "2026-10-28T10:00:00Z" }),
    ];
    expect(bannerJobs(succeededNewest)).toEqual([]);
  });

  test("没有任务或只有成功、取消时为空", () => {
    expect(bannerJobs([])).toEqual([]);
    expect(bannerJobs([job({ status: "canceled" })])).toEqual([]);
  });
});

describe("任务文案与落点", () => {
  test("进行中的话术", () => {
    expect(jobRunningText("breakdown")).toBe("正在拆解剧本资产…");
    expect(jobRunningText("analyze")).toBe("正在研读剧本…");
  });

  test("每个步骤对应的工序页", () => {
    expect(jobStage("analyze")).toBe("script");
    expect(jobStage("scripts")).toBe("script");
    expect(jobStage("breakdown")).toBe("assets");
    expect(jobStage("asset_prompts")).toBe("assets");
    expect(jobStage("storyboard")).toBe("studio");
    expect(jobStage("compose")).toBe("final");
  });
});

describe("两条线：pipeline 自动规划与 user 用户任务", () => {
  test("pipeline 线：研读 / 规划；没带 lane 的老任务按步骤判断", () => {
    expect(isPipelineJob(job({ lane: "pipeline", step: "plan" }))).toBe(true);
    expect(isPipelineJob(job({ lane: "user", step: "scripts" }))).toBe(false);
    expect(isPipelineJob(job({ lane: "user", step: "analyze" }))).toBe(true);
  });

  test("用户任务不含 pipeline；busyReason 不被规划拦住（互不阻塞）", () => {
    const plan = job({ id: "p", lane: "pipeline", step: "plan", episodes: [] });
    const scripts = job({ id: "s", step: "scripts", episodes: [1] });
    expect(userJobs([plan, scripts]).map((j) => j.id)).toEqual(["s"]);
    expect(busyReason([plan])).toBeNull();
    expect(busyReason([plan, scripts])).toContain("分集剧本");
  });

  test("规划任务标题", () => {
    expect(jobTitle(job({ step: "plan", episodes: [] }))).toBe("分集规划");
    expect(jobRunningText("plan")).toContain("规划");
  });
});

describe("JobBar 规划行 planningRow", () => {
  const drama = (patch: Record<string, unknown> = {}) => ({
    phase: "running" as const,
    planCursor: 0,
    rangeEnd: 1215,
    pauseReason: "",
    pauseCode: "" as const,
    unitKind: "chapter" as const,
    episodes: [] as { no: number }[],
    ...patch,
  });

  test("完成（done）没有规划行", () => {
    expect(planningRow(drama({ phase: "done" }), [])).toBeNull();
  });

  test("研读中：正在粗读全书", () => {
    const row = planningRow(drama(), [
      job({ lane: "pipeline", step: "analyze", episodes: [], progress: 30 }),
    ]);
    expect(row).toMatchObject({ kind: "running", title: "正在粗读全书", progress: 30 });
    expect(row?.kind === "running" && row.detail).toContain("研读");
  });

  test("规划中：正在规划第 a–b 章 · 已出 N 集目录，进度按规划游标 / 范围终点", () => {
    const row = planningRow(
      drama({
        planCursor: 300,
        rangeEnd: 1200,
        episodes: Array.from({ length: 52 }, (_, i) => ({ no: i + 1 })),
      }),
      [job({ lane: "pipeline", step: "plan", episodes: [], batchFrom: 301, batchTo: 312 })],
    );
    expect(row).toMatchObject({
      kind: "running",
      title: "正在规划第 301–312 章",
      progress: 25,
    });
    expect(row?.kind === "running" && row.detail).toContain("已出 52 集目录");
  });

  test("规划行的单位跟着单元种类：场 / 段 / 集", () => {
    const row = planningRow(drama({ unitKind: "scene" }), [
      job({ lane: "pipeline", step: "plan", episodes: [], batchFrom: 8, batchTo: 8 }),
    ]);
    expect(row?.kind === "running" && row.title).toBe("正在规划第 8 场");
  });

  test("两批之间还没有进行中任务：不报错，写「正在规划」", () => {
    const row = planningRow(drama({ planCursor: 12 }), []);
    expect(row).toMatchObject({ kind: "running", title: "正在规划" });
  });

  test("用户暂停：已规划到第 N 章，操作只有「继续」", () => {
    const row = planningRow(drama({ phase: "paused", pauseCode: "user", planCursor: 44 }), []);
    expect(row).toMatchObject({
      kind: "paused",
      tone: "muted",
      title: "分集规划已暂停",
      actions: ["resume"],
    });
    expect(row?.kind === "paused" && row.reason).toContain("已规划到第 44 章");
  });

  test("积分不足：充值后继续；失败 / 上下文不足：重试", () => {
    expect(planningRow(drama({ phase: "paused", pauseCode: "credits" }), [])).toMatchObject({
      tone: "error",
      reason: "积分不足，充值后继续",
      actions: ["resume"],
    });
    expect(
      planningRow(drama({ phase: "paused", pauseCode: "failed", pauseReason: "模型超时" }), []),
    ).toMatchObject({ title: "分集规划暂停", actions: ["retry"] });
    const failed = planningRow(
      drama({ phase: "paused", pauseCode: "failed", pauseReason: "模型超时" }),
      [],
    );
    expect(failed?.kind === "paused" && failed.reason).toContain("模型超时");
    expect(planningRow(drama({ phase: "paused", pauseCode: "context" }), [])).toMatchObject({
      actions: ["retry"],
    });
  });

  test("自定义提示词导致失败：多一个「改用内置重试」", () => {
    expect(planningRow(drama({ phase: "paused", pauseCode: "custom_prompt" }), [])).toMatchObject({
      tone: "error",
      actions: ["retry", "builtin"],
    });
  });

  test("没有暂停原因分类（老数据）按用户暂停处理", () => {
    expect(planningRow(drama({ phase: "paused" }), [])).toMatchObject({ actions: ["resume"] });
  });
});

describe("规划还要多久 planningEtaMinutes", () => {
  const hour = 3_600_000;
  const created = (now: number, agoMs: number) => new Date(now - agoMs).toISOString();

  test("按已规划的速度推算：1 小时规划了 300 个，还剩 900 个，约 180 分钟", () => {
    const now = Date.parse("2026-11-04T12:00:00Z");
    expect(
      planningEtaMinutes({ createdAt: created(now, hour), planCursor: 300, rangeEnd: 1200 }, now),
    ).toBe(180);
  });

  test("还没规划出任何单元、或已规划完、或时间不对：没有预估", () => {
    const now = Date.parse("2026-11-04T12:00:00Z");
    expect(
      planningEtaMinutes({ createdAt: created(now, hour), planCursor: 0, rangeEnd: 100 }, now),
    ).toBeNull();
    expect(
      planningEtaMinutes({ createdAt: created(now, hour), planCursor: 100, rangeEnd: 100 }, now),
    ).toBeNull();
    expect(
      planningEtaMinutes({ createdAt: created(now, -hour), planCursor: 10, rangeEnd: 100 }, now),
    ).toBeNull();
  });

  test("最少 1 分钟", () => {
    const now = Date.parse("2026-11-04T12:00:00Z");
    expect(
      planningEtaMinutes({ createdAt: created(now, 1000), planCursor: 99, rangeEnd: 100 }, now),
    ).toBe(1);
  });
});
