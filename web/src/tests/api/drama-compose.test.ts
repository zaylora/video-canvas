import { describe, expect, test } from "bun:test";

import { mapEpisodeSummary, mapJob, toJobBody } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, jobFailureMessage } from "@/api/drama/errors";
import type { BackendEpisodeSummary, BackendJobView } from "@/api/drama/type";
import { ApiError } from "@/utils/requests/request";

const rawAsset = {
  id: 42,
  kind: "video" as const,
  url: "https://cdn/final.mp4",
  mime_type: "video/mp4",
  byte_size: 100,
  width: 720,
  height: 1280,
  duration_ms: 30000,
  file_name: "final.mp4",
};

const rawEpisode = (patch: Partial<BackendEpisodeSummary> = {}): BackendEpisodeSummary => ({
  no: 1,
  title: "第一集",
  synopsis: "",
  script_status: "ready",
  locked: true,
  revision: 1,
  updated_at: "x",
  ...patch,
});

const rawJob = (patch: Partial<BackendJobView> = {}): BackendJobView => ({
  id: "j1",
  step: "compose",
  episodes: [1],
  status: "succeeded",
  progress: 100,
  credits: 0,
  error_code: 0,
  error_message: "",
  created_at: "x",
  finished_at: "y",
  ...patch,
});

describe("契约 §14：集摘要的成片字段", () => {
  test("final / final_stale 映射；后端没带时按「没有成片、没过期」", () => {
    const bare = mapEpisodeSummary(rawEpisode());
    expect(bare.final).toBeNull();
    expect(bare.finalStale).toBe(false);
    const done = mapEpisodeSummary(rawEpisode({ final: rawAsset, final_stale: true }));
    expect(done.final).toMatchObject({ id: "42", url: "https://cdn/final.mp4", durationMs: 30000 });
    expect(done.finalStale).toBe(true);
  });
});

describe("契约 §14：JobView.result", () => {
  test("compose 成功带 asset 与 skipped_seqs", () => {
    const job = mapJob(rawJob({ result: { asset: rawAsset, skipped_seqs: [2] } }));
    expect(job.result?.asset.url).toBe("https://cdn/final.mp4");
    expect(job.result?.skippedSeqs).toEqual([2]);
  });
  test("其他步骤 result 为 null / 缺省；skipped_seqs 为 null 当空数组", () => {
    expect(mapJob(rawJob({ step: "storyboard", result: null })).result).toBeNull();
    expect(mapJob(rawJob()).result).toBeNull();
    expect(
      mapJob(rawJob({ result: { asset: rawAsset, skipped_seqs: null } })).result?.skippedSeqs,
    ).toEqual([]);
  });
  test("提交 compose 只发 step 与 episodes", () => {
    expect(toJobBody({ step: "compose", episodes: [3], maxSeconds: 15 })).toEqual({
      step: "compose",
      episodes: [3],
    });
  });
});

describe("契约 §16：67010", () => {
  test("文案与失败任务说明", () => {
    expect(DRAMA_ERR.nothingToCompose).toBe(67010);
    const error = new ApiError("", 67010, 400);
    expect(dramaErrorMessage(error)).toBe("这一集还没有可合成的片段");
    expect(jobFailureMessage({ errorCode: 67010, errorMessage: "" })).toBe(
      "这一集还没有可合成的片段",
    );
  });
});
