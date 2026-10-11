import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import type { CreateTaskItem, CreateTaskRequest } from "@/api/generation-task/type";
import type { Capabilities } from "@/api/model/type";
import type { CanvasNodeData } from "@/types";
import { frameByMult } from "@/utils/canvas/outpaint-frame";
import {
  createOutpaintRunner,
  type OutpaintDeps,
  type OutpaintJob,
  type PatchNode,
} from "@/utils/canvas/outpaint-run";

const caps: Capabilities = {
  ops: ["t2i", "i2i"],
  refs: {
    image: { on: true, max: 4, max_mb: 10 },
    video: { on: false, max: 0, max_mb: 0 },
    audio: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 1000 },
  params: {
    aspect_ratio: {
      type: "enum",
      label: "比例",
      open: true,
      options: ["1:1", "16:9", "9:16"],
      default: "1:1",
    },
  },
};

const origin = { w: 576, h: 324 };

const job = (over: Partial<OutpaintJob> = {}): OutpaintJob => ({
  nodeId: "n2",
  sourceSrc: "https://cdn/a.png",
  origin,
  frame: frameByMult(origin, 1.5),
  caps,
  modelKey: "img-1",
  canvasId: "c1",
  params: { prompt: "完整提示词", op: "i2i", aspect_ratio: "16:9" },
  ...over,
});

const asset: AssetDto = {
  id: "77",
  url: "https://cdn/composite.png",
  kind: "image",
  mimeType: "image/png",
  byteSize: 1000,
  width: 2880,
  height: 1620,
  durationMs: null,
  fileName: "outpaint.png",
};

/** 记录所有交互的假依赖 */
function setup(over: Partial<OutpaintDeps> = {}) {
  const log: string[] = [];
  const patches: Partial<CanvasNodeData>[] = [];
  let data: CanvasNodeData = {
    kind: "image",
    label: "图片 1-扩图",
    status: "running",
    params: { prompt: "完整提示词", op: "i2i", aspect_ratio: "16:9" },
  };
  const submits: CreateTaskRequest[] = [];
  const deps: OutpaintDeps = {
    load: async () => {
      log.push("load");
      return { image: {} as CanvasImageSource, width: 1920, height: 1080 };
    },
    compose: async () => {
      log.push("compose");
      return new Blob(["x".repeat(100)], { type: "image/png" });
    },
    upload: async (file) => {
      log.push(`upload:${file.name}:${file.type}`);
      return asset;
    },
    submit: async (request) => {
      log.push("submit");
      submits.push(request);
      const item: CreateTaskItem = {
        node_id: request.node_id,
        task: { id: 501 } as CreateTaskItem["task"],
      };
      return [item];
    },
    ...over,
  };
  const patch: PatchNode = (_id, update) => {
    const next = update(data);
    patches.push(next);
    data = { ...data, ...next };
  };
  return { deps, patch, log, patches, submits, data: () => data };
}

describe("OutpaintRunner 成功路径", () => {
  test("读图 → 合成 → 上传 → 提交，参考图是合成图素材，节点写入 taskId", async () => {
    const t = setup();
    const runner = createOutpaintRunner(t.deps);
    await runner.start(job(), t.patch);
    expect(t.log).toEqual(["load", "compose", "upload:outpaint.png:image/png", "submit"]);

    expect(t.submits).toHaveLength(1);
    const request = t.submits[0];
    expect(request).toMatchObject({
      kind: "image",
      model_id: "img-1",
      canvas_id: "c1",
      node_id: "n2",
    });
    expect(request.input).toMatchObject({
      op: "i2i",
      images: [77],
      prompt: "完整提示词",
      aspect_ratio: "16:9",
    });

    const node = t.data();
    expect(node.taskId).toBe("501");
    expect(node.status).toBe("running");
    expect((node.params as Record<string, unknown>).images).toEqual(["77"]);
    expect(node.paramAssets?.["77"]).toMatchObject({ url: asset.url, mediaType: "image" });
  });

  test("合成图素材在提交前就写进节点，提交失败后普通重试也有参考图", async () => {
    const t = setup({
      submit: async (request) => [
        { node_id: request.node_id, error: { status: 402, code: 40001, message: "x" } },
      ],
    });
    const runner = createOutpaintRunner(t.deps);
    await runner.start(job(), t.patch);
    const node = t.data();
    expect(node.status).toBe("error");
    expect(node.error).toBe("积分不足，无法生成");
    expect((node.params as Record<string, unknown>).images).toEqual(["77"]);
    expect(node.taskId).toBeUndefined();
    // 已经有合成图了，不需要也没有扩图重试材料
    expect(runner.canRetry("n2")).toBe(false);
  });

  test("运行中 isActive 为真，结束后为假", async () => {
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    const t = setup({
      load: async () => {
        await gate;
        return { image: {} as CanvasImageSource, width: 1920, height: 1080 };
      },
    });
    const runner = createOutpaintRunner(t.deps);
    const running = runner.start(job(), t.patch);
    expect(runner.isActive("n2")).toBe(true);
    release();
    await running;
    expect(runner.isActive("n2")).toBe(false);
  });
});

describe("OutpaintRunner 失败与重试", () => {
  test("读不到原图：节点报错，不上传不提交，可以重试", async () => {
    let fail = true;
    const t = setup({
      load: async () => {
        if (fail) throw new Error("无法读取原图");
        return { image: {} as CanvasImageSource, width: 1920, height: 1080 };
      },
    });
    const runner = createOutpaintRunner(t.deps);
    await runner.start(job(), t.patch);
    expect(t.data().status).toBe("error");
    expect(t.data().error).toBe("无法读取原图");
    expect(t.log).toEqual([]);
    expect(runner.canRetry("n2")).toBe(true);

    fail = false;
    expect(await runner.retry("n2", t.patch)).toBe(true);
    expect(t.data().status).toBe("running");
    expect(t.data().taskId).toBe("501");
    expect(t.data().error).toBeNull();
    expect(runner.canRetry("n2")).toBe(false);
  });

  test("合成图超过模型参考图大小上限：提示并停下，不上传", async () => {
    const t = setup({
      compose: async () => new Blob([new Uint8Array(11 * 1024 * 1024)], { type: "image/png" }),
    });
    const runner = createOutpaintRunner(t.deps);
    await runner.start(job(), t.patch);
    expect(t.data().status).toBe("error");
    expect(t.data().error).toContain("10 MB");
    expect(t.log).toEqual(["load"]);
  });

  test("上传失败：节点报错，可以重试，不提交", async () => {
    const t = setup({
      upload: async () => {
        throw new Error("boom");
      },
    });
    const runner = createOutpaintRunner(t.deps);
    await runner.start(job(), t.patch);
    expect(t.data().status).toBe("error");
    expect(t.data().error).toBe("合成图上传失败，请重试");
    expect(t.submits).toEqual([]);
    expect(runner.canRetry("n2")).toBe(true);
  });

  test("模型必填参数没填会在提交前报错，不发请求", async () => {
    const strict: Capabilities = {
      ...caps,
      params: { quality: { type: "enum", label: "质量", open: true, options: ["hd"] } },
    };
    const t = setup();
    const runner = createOutpaintRunner(t.deps);
    await runner.start(job({ caps: strict }), t.patch);
    expect(t.data().status).toBe("error");
    expect(t.data().error).toBe("请填写质量");
    expect(t.submits).toEqual([]);
  });

  test("没有重试材料（刷新后）时 retry 返回 false", async () => {
    const t = setup();
    const runner = createOutpaintRunner(t.deps);
    expect(await runner.retry("nope", t.patch)).toBe(false);
    expect(runner.canRetry("nope")).toBe(false);
  });
});
