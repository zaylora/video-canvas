import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import {
  addSegment,
  deleteEpisode,
  deleteSegment,
  generateSegment,
  listSegments,
  mapEpisodeSummary,
  mapSegment,
  reorderSegments,
  toJobBody,
  toSegmentUpdateBody,
  updateSegment,
} from "@/api/drama";
import type { BackendSegmentView } from "@/api/drama/type";
import service from "@/utils/requests/service";

const rawAsset = (id: number, url = `https://cdn/${id}.mp4`) => ({
  id,
  kind: "video" as const,
  url,
  mime_type: "video/mp4",
  byte_size: 10,
  width: 720,
  height: 1280,
  duration_ms: 8000,
  file_name: null,
});

const rawSegment = (patch: Partial<BackendSegmentView> = {}): BackendSegmentView => ({
  id: "5d2a",
  episode_no: 1,
  seq: 0,
  seconds: 12,
  prompt: "本片段场景设定在 @[山村街道](n_abc)。",
  takes: [
    {
      task_id: "123",
      status: "succeeded",
      asset: rawAsset(9),
      error_message: "",
      created_at: "2026-10-28T10:00:00Z",
    },
    {
      task_id: "124",
      status: "failed",
      asset: null,
      error_message: "内容审核未通过",
      created_at: "2026-10-28T10:05:00Z",
    },
  ],
  chosen_task_id: "123",
  status: "done",
  revision: 1,
  updated_at: "2026-10-28T10:05:00Z",
  ...patch,
});

describe("api/drama：片段 mapper（契约 §9）", () => {
  test("片段 snake→camel：分镜、take、素材一并映射", () => {
    const seg = mapSegment(rawSegment());
    expect(seg).toMatchObject({
      id: "5d2a",
      episodeNo: 1,
      seq: 0,
      seconds: 12,
      chosenTaskId: "123",
      status: "done",
      revision: 1,
    });
    // 片段不再带分镜表
    expect(seg).not.toHaveProperty("shots");
    expect(seg.takes[0].asset?.url).toBe("https://cdn/9.mp4");
    expect(seg.takes[0].asset?.id).toBe("9");
    expect(seg.takes[1]).toMatchObject({ taskId: "124", status: "failed", asset: null });
    expect(seg.takes[1].errorMessage).toBe("内容审核未通过");
  });

  test("takes 为 null、task_id 是数字时兜底", () => {
    const seg = mapSegment(
      rawSegment({
        takes: null,
        chosen_task_id: undefined as unknown as string,
      }),
    );
    expect(seg.takes).toEqual([]);
    expect(seg.chosenTaskId).toBe("");
    const num = mapSegment(
      rawSegment({
        takes: [
          {
            task_id: 77 as unknown as string,
            status: "running",
            asset: null,
            error_message: "",
            created_at: "x",
          },
        ],
      }),
    );
    expect(num.takes[0].taskId).toBe("77");
  });

  test("集摘要：二期字段缺省时按空处理，有值时映射", () => {
    const base = {
      no: 1,
      title: "t",
      synopsis: "",
      script_status: "ready" as const,
      locked: true,
      revision: 1,
      updated_at: "x",
    };
    expect(mapEpisodeSummary(base)).toMatchObject({
      storyboardStatus: "empty",
      segmentCount: 0,
      segmentSeconds: 0,
      doneSegmentCount: 0,
    });
    expect(
      mapEpisodeSummary({
        ...base,
        storyboard_status: "ready",
        segment_count: 4,
        segment_seconds: 50,
        done_segment_count: 1,
      }),
    ).toMatchObject({
      storyboardStatus: "ready",
      segmentCount: 4,
      segmentSeconds: 50,
      doneSegmentCount: 1,
    });
  });
});

describe("api/drama：片段请求体", () => {
  test("改片段只带传了的字段，chosenTaskId 转 snake_case", () => {
    expect(toSegmentUpdateBody({ revision: 2, prompt: "新" })).toEqual({
      revision: 2,
      prompt: "新",
    });
    expect(toSegmentUpdateBody({ revision: 2, seconds: 8, chosenTaskId: "123" })).toEqual({
      revision: 2,
      seconds: 8,
      chosen_task_id: "123",
    });
  });

  test("分镜任务带 max_seconds，其他步骤不带", () => {
    expect(toJobBody({ step: "storyboard", episodes: [1], maxSeconds: 10 })).toEqual({
      step: "storyboard",
      episodes: [1],
      max_seconds: 10,
    });
    expect(toJobBody({ step: "scripts", episodes: [1] })).toEqual({
      step: "scripts",
      episodes: [1],
    });
  });
});

describe("api/drama：片段请求", () => {
  let get: Mock<typeof service.get>;
  let post: Mock<typeof service.post>;
  let patch: Mock<typeof service.patch>;
  let put: Mock<typeof service.put>;
  let del: Mock<typeof service.delete>;
  beforeEach(() => {
    get = spyOn(service, "get");
    post = spyOn(service, "post");
    patch = spyOn(service, "patch");
    put = spyOn(service, "put");
    del = spyOn(service, "delete");
  });
  afterEach(() => {
    get.mockRestore();
    post.mockRestore();
    patch.mockRestore();
    put.mockRestore();
    del.mockRestore();
  });

  test("列表：items 为 null 兜底", async () => {
    get.mockResolvedValue({ items: null });
    expect(await listSegments("1a2b", 1)).toEqual([]);
    expect(get.mock.calls[0][0]).toBe("/dramas/1a2b/episodes/1/segments");
    get.mockResolvedValue({ items: [rawSegment()] });
    expect((await listSegments("1a2b", 1))[0].id).toBe("5d2a");
  });

  test("新增：after_seq 映射；改静默（409 由调用方处理）", async () => {
    post.mockResolvedValue(rawSegment());
    await addSegment("1a2b", 2, { prompt: "p", seconds: 8, afterSeq: 1 });
    expect(post.mock.calls[0][0]).toBe("/dramas/1a2b/episodes/2/segments");
    expect(post.mock.calls[0][1]).toEqual({ prompt: "p", seconds: 8, after_seq: 1 });
    patch.mockResolvedValue(rawSegment());
    await updateSegment("5d2a", { revision: 1, chosenTaskId: "123" });
    expect(patch.mock.calls[0][0]).toBe("/drama-segments/5d2a");
    expect(patch.mock.calls[0][1]).toEqual({ revision: 1, chosen_task_id: "123" });
    expect(patch.mock.calls[0][2]).toMatchObject({ silent: true });
  });

  test("删除与重排", async () => {
    del.mockResolvedValue(undefined);
    await deleteSegment("5d2a");
    expect(del.mock.calls[0][0]).toBe("/drama-segments/5d2a");
    await deleteEpisode("1a2b", 3);
    expect(del.mock.calls[1][0]).toBe("/dramas/1a2b/episodes/3");
    // 失败原因（有任务在跑等）由调用方就地提示，不弹全局 toast
    expect(del.mock.calls[1][1]).toMatchObject({ silent: true });
    put.mockResolvedValue({ items: [rawSegment({ id: "b" }), rawSegment({ id: "a" })] });
    const list = await reorderSegments("1a2b", 1, ["b", "a"]);
    expect(put.mock.calls[0][0]).toBe("/dramas/1a2b/episodes/1/segments/order");
    expect(put.mock.calls[0][1]).toEqual({ ids: ["b", "a"] });
    expect(list.map((s) => s.id)).toEqual(["b", "a"]);
  });

  test("generate：带幂等键，返回任务快照", async () => {
    post.mockResolvedValue({ task: { id: "1", status: "pending" } });
    const body = { model_id: "veo", input: { prompt: "x" } };
    const result = await generateSegment("5d2a", body, "key-1");
    expect(post.mock.calls[0][0]).toBe("/drama-segments/5d2a/generate");
    expect(post.mock.calls[0][1]).toEqual(body);
    expect(post.mock.calls[0][2]).toMatchObject({ headers: { "Idempotency-Key": "key-1" } });
    expect(result.task.id).toBe("1");
  });
});
