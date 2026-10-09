import { describe, expect, test } from "bun:test";

import type { RecordDto } from "@/api/conversation/type";
import { restoreComposer } from "@/utils/conversation/restore";

const record = (input: Record<string, unknown>, over: Partial<RecordDto> = {}): RecordDto => ({
  id: "r1",
  conversationId: "c1",
  kind: "video",
  modelId: "seedance",
  prompt: "镜头缓慢推进",
  input,
  count: 2,
  tasks: [null, null],
  submitErrors: [],
  quoteCredits: 0,
  createdAt: "2026-10-09T08:00:00Z",
  ...over,
});

describe("restoreComposer：从记录快照还原输入卡片", () => {
  test("模式、模型、提示词来自记录；参数去掉提示词和参考素材键，生成方式保留", () => {
    const restored = restoreComposer(
      record({
        prompt: "镜头缓慢推进（展开后）",
        op: "i2v",
        aspect_ratio: "16:9",
        duration: 5,
        count: 2,
        images: [11, 12],
      }),
    );
    expect(restored.mode).toBe("video");
    expect(restored.modelId).toBe("seedance");
    expect(restored.text).toBe("镜头缓慢推进");
    expect(restored.params).toEqual({ op: "i2v", aspect_ratio: "16:9", duration: 5, count: 2 });
    expect(restored.refs).toEqual([
      { kind: "image", id: 11 },
      { kind: "image", id: 12 },
    ]);
  });

  test("没有参考时 refs 为空；素材 id 去重，非法值丢掉", () => {
    expect(restoreComposer(record({})).refs).toEqual([]);
    expect(restoreComposer(record({ images: [3, 3, "x", 0, "7"] })).refs).toEqual([
      { kind: "image", id: 3 },
      { kind: "image", id: 7 },
    ]);
    expect(restoreComposer(record({ images: "nope" })).refs).toEqual([]);
  });

  test("图片、视频、音频参考都还原，并保留各自的种类", () => {
    const restored = restoreComposer(record({ images: [1], videos: [2], audios: [3, 4] }));
    expect(restored.refs).toEqual([
      { kind: "image", id: 1 },
      { kind: "video", id: 2 },
      { kind: "audio", id: 3 },
      { kind: "audio", id: 4 },
    ]);
    expect(restored.params).toEqual({});
  });

  test("提示词用记录里保存的原文，不用 input.prompt（后者可能已把引用展开）", () => {
    expect(restoreComposer(record({ prompt: "展开后的文字" })).text).toBe("镜头缓慢推进");
  });
});
