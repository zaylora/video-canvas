import { describe, expect, test } from "bun:test";

import type { DramaJob } from "@/api/drama/type";
import {
  busyReason,
  isActiveJob,
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
