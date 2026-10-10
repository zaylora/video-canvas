import { describe, expect, test } from "bun:test";

import type { AssetDto, UploadOptions } from "@/api/asset/type";
import type { CanvasNodeData } from "@/types";
import { createUploadRunner } from "@/utils/canvas/upload-runner";

const file = () => new File(["x"], "a.png", { type: "image/png" });

const asset = (id = "7"): AssetDto => ({
  id,
  url: `/assets/${id}.png`,
  kind: "image",
  mimeType: "image/png",
  byteSize: 1,
  width: null,
  height: null,
  durationMs: null,
  fileName: "a.png",
});

/** 一次可控的上传：测试决定什么时候报进度、成功还是失败 */
function controlled() {
  const calls: {
    file: File;
    options: UploadOptions;
    ok: (value: AssetDto) => void;
    fail: (error: unknown) => void;
  }[] = [];
  const upload = (f: File, options: UploadOptions = {}) =>
    new Promise<AssetDto>((resolve, reject) => {
      calls.push({ file: f, options, ok: resolve, fail: reject });
      options.signal?.addEventListener("abort", () =>
        reject(new DOMException("上传已取消", "AbortError")),
      );
    });
  return { calls, upload };
}

/** 记下对节点数据的每一次改动 */
function patches() {
  const log: { id: string; patch: Partial<CanvasNodeData> }[] = [];
  return { log, update: (id: string, patch: Partial<CanvasNodeData>) => log.push({ id, patch }) };
}

describe("上传流程：开始、进度、成功", () => {
  test("开始时节点进入上传中，进度从 0 起，清掉旧的错误", () => {
    const { upload } = controlled();
    const runner = createUploadRunner(upload);
    const { log, update } = patches();
    void runner.start("n1", file(), update);
    expect(log[0]).toEqual({
      id: "n1",
      patch: { status: "running", uploadProgress: 0, error: null },
    });
    expect(runner.isActive("n1")).toBe(true);
  });

  test("进度只在整数百分比变化时才更新节点", () => {
    const { upload, calls } = controlled();
    const runner = createUploadRunner(upload);
    const { log, update } = patches();
    void runner.start("n1", file(), update);
    for (const percent of [0, 0, 5, 5, 5, 6]) calls[0].options.onProgress?.(percent);
    expect(log.slice(1).map((item) => item.patch.uploadProgress)).toEqual([5, 6]);
  });

  test("成功：写入地址和素材 id，状态变 done，去掉上传进度，不再占着上传", async () => {
    const { upload, calls } = controlled();
    const runner = createUploadRunner(upload);
    const { log, update } = patches();
    const result = runner.start("n1", file(), update);
    calls[0].ok(asset("9"));
    expect(await result).toBe("done");
    expect(log.at(-1)?.patch).toEqual({
      status: "done",
      src: "/assets/9.png",
      assetId: "9",
      uploadProgress: undefined,
    });
    expect(runner.isActive("n1")).toBe(false);
    expect(runner.canRetry("n1")).toBe(false);
  });
});

describe("上传流程：失败与重试", () => {
  test("失败：节点变 error 并写明原因，文件留着等重试", async () => {
    const { upload, calls } = controlled();
    const runner = createUploadRunner(upload);
    const { log, update } = patches();
    const result = runner.start("n1", file(), update);
    calls[0].fail(new Error("boom"));
    expect(await result).toBe("failed");
    expect(log.at(-1)?.patch).toEqual({
      status: "error",
      error: "上传失败，请重试",
      uploadProgress: undefined,
    });
    expect(runner.isActive("n1")).toBe(false);
    expect(runner.canRetry("n1")).toBe(true);
  });

  test("重试：用留着的文件在同一个节点上重新开始", async () => {
    const { upload, calls } = controlled();
    const runner = createUploadRunner(upload);
    const { log, update } = patches();
    const original = file();
    const first = runner.start("n1", original, update);
    calls[0].fail(new Error("boom"));
    await first;

    const second = runner.retry("n1", update);
    expect(calls).toHaveLength(2);
    expect(calls[1].file).toBe(original);
    expect(log.at(-1)).toEqual({
      id: "n1",
      patch: { status: "running", uploadProgress: 0, error: null },
    });
    calls[1].ok(asset());
    expect(await second).toBe("done");
  });

  test("没有留着的文件（比如刷新后）无法重试", async () => {
    const { upload } = controlled();
    const runner = createUploadRunner(upload);
    expect(runner.canRetry("ghost")).toBe(false);
    expect(await runner.retry("ghost", () => {})).toBe("failed");
  });
});

describe("上传流程：取消", () => {
  test("取消会中止请求，节点不再被改动，也没法再重试", async () => {
    const { upload, calls } = controlled();
    const runner = createUploadRunner(upload);
    const { log, update } = patches();
    const result = runner.start("n1", file(), update);
    const before = log.length;
    runner.cancel("n1");
    expect(calls[0].options.signal?.aborted).toBe(true);
    expect(await result).toBe("aborted");
    expect(log).toHaveLength(before);
    expect(runner.isActive("n1")).toBe(false);
    expect(runner.canRetry("n1")).toBe(false);
  });

  test("取消失败的节点：丢掉暂存的文件", async () => {
    const { upload, calls } = controlled();
    const runner = createUploadRunner(upload);
    const result = runner.start("n1", file(), () => {});
    calls[0].fail(new Error("boom"));
    await result;
    runner.cancel("n1");
    expect(runner.canRetry("n1")).toBe(false);
  });

  test("取消全部：每个在传的都中止", () => {
    const { upload, calls } = controlled();
    const runner = createUploadRunner(upload);
    void runner.start("a", file(), () => {});
    void runner.start("b", file(), () => {});
    runner.cancelAll();
    expect(calls.map((call) => call.options.signal?.aborted)).toEqual([true, true]);
  });
});
