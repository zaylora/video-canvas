import { describe, expect, test } from "bun:test";

import type { DramaJob, EpisodeSummary, Segment } from "@/api/drama/type";
import {
  activeComposeJob,
  composeCheck,
  composeCheckEpisode,
  composeNotice,
  finalState,
  missingVideoSeqs,
  skippedText,
} from "@/utils/drama/compose";
import { jobTitle } from "@/utils/drama/jobs";

const asset = (id: string, url = `https://cdn/${id}.mp4`) => ({
  id,
  url,
  kind: "video" as const,
  mimeType: "video/mp4",
  byteSize: 1,
  width: null,
  height: null,
  durationMs: null,
  fileName: null,
});

const seg = (id: string, seq: number, withVideo: boolean): Segment => ({
  id,
  episodeNo: 1,
  seq,
  seconds: 10,
  prompt: "",
  takes: withVideo
    ? [
        {
          taskId: `t${id}`,
          status: "succeeded",
          asset: asset(id),
          errorMessage: "",
          createdAt: "x",
        },
      ]
    : [],
  chosenTaskId: withVideo ? `t${id}` : "",
  status: "idle",
  revision: 1,
  updatedAt: "x",
});

const episode = (patch: Partial<EpisodeSummary> = {}): EpisodeSummary => ({
  no: 1,
  title: "第一集",
  synopsis: "",
  scriptStatus: "ready",
  locked: true,
  revision: 1,
  updatedAt: "x",
  storyboardStatus: "ready",
  segmentCount: 3,
  segmentSeconds: 30,
  doneSegmentCount: 2,
  final: null,
  finalStale: false,
  ...patch,
});

const job = (patch: Partial<DramaJob> = {}): DramaJob => ({
  id: "j1",
  step: "compose",
  episodes: [1],
  status: "running",
  progress: 30,
  credits: 0,
  errorCode: 0,
  errorMessage: "",
  createdAt: "2026-10-28T10:00:00Z",
  finishedAt: null,
  result: null,
  ...patch,
});

describe("合成整集按钮可用性", () => {
  test("至少一个片段有采用视频才可点", () => {
    const check = composeCheck([seg("a", 0, false), seg("b", 1, true)], null);
    expect(check).toEqual({ enabled: true, reason: null });
  });

  test("一个采用视频都没有：禁用并写原因", () => {
    const check = composeCheck([seg("a", 0, false)], null);
    expect(check.enabled).toBe(false);
    expect(check.reason).toBe("至少要有一个片段采用了视频才能合成");
  });

  test("没有片段：同样禁用", () => {
    expect(composeCheck([], null).enabled).toBe(false);
  });

  test("有进行中的任务：禁用并带上任务原因（同一部短剧同时只有一个任务）", () => {
    const check = composeCheck([seg("a", 0, true)], "有进行中的任务：分集剧本 · 3 集");
    expect(check).toEqual({ enabled: false, reason: "有进行中的任务：分集剧本 · 3 集" });
  });
});

describe("分集卡片上的合成判定", () => {
  test("集摘要里有采用视频的片段才可点；有进行中的任务时禁用", () => {
    expect(composeCheckEpisode(episode({ doneSegmentCount: 1 }), null).enabled).toBe(true);
    expect(composeCheckEpisode(episode({ doneSegmentCount: 0 }), null).enabled).toBe(false);
    expect(composeCheckEpisode(episode({ doneSegmentCount: 2 }), "有进行中的任务：x")).toEqual({
      enabled: false,
      reason: "有进行中的任务：x",
    });
  });
});

describe("没有采用视频的片段（合成时会被跳过）", () => {
  test("missingVideoSeqs 返回 0 起的 seq，按顺序", () => {
    expect(missingVideoSeqs([seg("a", 0, false), seg("b", 1, true), seg("c", 2, false)])).toEqual([
      0, 2,
    ]);
  });

  test("skippedText 用界面上的序号（seq+1，两位）", () => {
    expect(skippedText([1, 3])).toBe("02、04");
    expect(skippedText([])).toBe("");
  });
});

describe("成片状态 finalState", () => {
  test("没有成片", () => {
    expect(finalState(episode())).toBe("none");
  });
  test("有成片且没过期", () => {
    expect(finalState(episode({ final: asset("f") }))).toBe("fresh");
  });
  test("有成片但片段之后改过：已过期", () => {
    expect(finalState(episode({ final: asset("f"), finalStale: true }))).toBe("stale");
  });
  test("没有成片时 finalStale 无意义，仍是 none", () => {
    expect(finalState(episode({ finalStale: true }))).toBe("none");
  });
});

describe("合成任务", () => {
  test("activeComposeJob 只认这一集进行中的合成任务", () => {
    const jobs = [
      job({ id: "a", episodes: [2] }),
      job({ id: "b", step: "storyboard" }),
      job({ id: "c", status: "succeeded" }),
      job({ id: "d" }),
    ];
    expect(activeComposeJob(jobs, 1)?.id).toBe("d");
    expect(activeComposeJob(jobs, 3)).toBeNull();
  });

  test("jobTitle 带步骤名和集号", () => {
    expect(jobTitle(job())).toBe("合成整集 · 第 1 集");
  });

  test("composeNotice：成功且跳过了片段才提示，带上序号", () => {
    const done = job({
      status: "succeeded",
      result: { asset: asset("f"), skippedSeqs: [1, 3] },
    });
    expect(composeNotice(done)).toBe("第 1 集已合成，跳过了没有采用视频的片段 02、04");
  });

  test("composeNotice：没跳过、非合成、未成功都不提示", () => {
    expect(
      composeNotice(job({ status: "succeeded", result: { asset: asset("f"), skippedSeqs: [] } })),
    ).toBeNull();
    expect(composeNotice(job({ status: "running" }))).toBeNull();
    expect(
      composeNotice(job({ step: "storyboard", status: "succeeded", result: null })),
    ).toBeNull();
  });
});
