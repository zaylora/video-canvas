import { describe, expect, test } from "bun:test";

import type { AssetDto, UploadOptions } from "@/api/asset/type";
import type { LibraryItemDto } from "@/api/asset-library/type";
import { createUploadQueue, type UploadDeps } from "@/utils/assets/upload-queue";

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

const asset = (id: string): AssetDto => ({
  id,
  url: `/files/${id}`,
  kind: "image",
  mimeType: "image/png",
  byteSize: 1,
  width: null,
  height: null,
  durationMs: null,
  fileName: `${id}.png`,
});

const file = (name: string) => new File(["x"], name, { type: "image/png" });

type Pending = {
  file: File;
  options: UploadOptions;
  resolve: (asset: AssetDto) => void;
  reject: (error: unknown) => void;
};

/** 假依赖：上传由测试手动放行，登记默认成功，可让它失败 */
function harness(options: { registerFails?: () => boolean; concurrency?: number } = {}) {
  const uploads: Pending[] = [];
  const registered: Array<{ assetId: string; folderId: string | null }> = [];
  const done: LibraryItemDto[] = [];
  const deps: UploadDeps = {
    upload: (target, opts) =>
      new Promise<AssetDto>((resolve, reject) => {
        uploads.push({ file: target, options: opts, resolve, reject });
      }),
    register: async (assetId, folderId) => {
      if (options.registerFails?.()) throw new Error("登记失败");
      registered.push({ assetId, folderId });
      return { id: `i${assetId}`, asset: asset(assetId), folderId, createdAt: "" };
    },
    isAborted: (error) => (error as { name?: string })?.name === "AbortError",
  };
  const queue = createUploadQueue(deps, (item) => done.push(item), options.concurrency);
  return { queue, uploads, registered, done };
}

describe("createUploadQueue：资产库上传", () => {
  test("多个文件逐个入队，带着目标文件夹", () => {
    const { queue, uploads } = harness();
    queue.add([file("a.png"), file("b.png")], "f1");
    const tasks = queue.getState();
    expect(tasks.map((task) => task.file.name)).toEqual(["a.png", "b.png"]);
    expect(tasks.every((task) => task.folderId === "f1")).toBe(true);
    expect(uploads).toHaveLength(2);
  });

  test("上传进度写进任务", () => {
    const { queue, uploads } = harness();
    queue.add([file("a.png")], null);
    uploads[0]!.options.onProgress?.(42);
    expect(queue.getState()[0]).toMatchObject({ status: "uploading", percent: 42 });
  });

  test("上传完成后登记到文件夹，通知完成并把任务移出队列", async () => {
    const { queue, uploads, registered, done } = harness();
    queue.add([file("a.png")], "f1");
    uploads[0]!.resolve(asset("7"));
    await tick();
    expect(registered).toEqual([{ assetId: "7", folderId: "f1" }]);
    expect(done.map((item) => item.id)).toEqual(["i7"]);
    expect(queue.getState()).toEqual([]);
  });

  test("上传失败显示原因，重试后成功", async () => {
    const { queue, uploads, done } = harness();
    queue.add([file("a.png")], null);
    uploads[0]!.reject(new Error("文件太大"));
    await tick();
    expect(queue.getState()[0]).toMatchObject({ status: "failed", error: "文件太大" });

    queue.retry(queue.getState()[0]!.id);
    expect(queue.getState()[0]).toMatchObject({ status: "uploading", percent: 0, error: null });
    expect(uploads).toHaveLength(2);
    uploads[1]!.resolve(asset("8"));
    await tick();
    expect(done).toHaveLength(1);
    expect(queue.getState()).toEqual([]);
  });

  test("没有错误信息的失败也有兜底文案", async () => {
    const { queue, uploads } = harness();
    queue.add([file("a.png")], null);
    uploads[0]!.reject("weird");
    await tick();
    expect(queue.getState()[0]!.error).toBe("上传失败");
  });

  test("登记失败时重试只重新登记，不再重传文件", async () => {
    let failRegister = true;
    const { queue, uploads, registered } = harness({ registerFails: () => failRegister });
    queue.add([file("a.png")], "f1");
    uploads[0]!.resolve(asset("7"));
    await tick();
    expect(queue.getState()[0]).toMatchObject({ status: "failed", error: "登记失败" });

    failRegister = false;
    queue.retry(queue.getState()[0]!.id);
    await tick();
    expect(uploads).toHaveLength(1);
    expect(registered).toEqual([{ assetId: "7", folderId: "f1" }]);
    expect(queue.getState()).toEqual([]);
  });

  test("取消会中止请求并移出队列，取消不算失败", async () => {
    const { queue, uploads, done } = harness();
    queue.add([file("a.png")], null);
    const id = queue.getState()[0]!.id;
    const signal = uploads[0]!.options.signal!;
    queue.cancel(id);
    expect(signal.aborted).toBe(true);
    expect(queue.getState()).toEqual([]);
    uploads[0]!.reject(Object.assign(new Error("cancel"), { name: "AbortError" }));
    await tick();
    expect(queue.getState()).toEqual([]);
    expect(done).toEqual([]);
  });

  test("失败的任务可以被丢弃", async () => {
    const { queue, uploads } = harness();
    queue.add([file("a.png")], null);
    uploads[0]!.reject(new Error("boom"));
    await tick();
    queue.cancel(queue.getState()[0]!.id);
    expect(queue.getState()).toEqual([]);
  });

  test("并发有上限，前面的结束后排队的才开始", async () => {
    const { queue, uploads } = harness({ concurrency: 2 });
    queue.add([file("a.png"), file("b.png"), file("c.png")], null);
    expect(uploads).toHaveLength(2);
    expect(queue.getState().map((task) => task.status)).toEqual([
      "uploading",
      "uploading",
      "queued",
    ]);
    uploads[0]!.resolve(asset("1"));
    await tick();
    expect(uploads).toHaveLength(3);
  });

  test("setOnDone 换掉完成回调：之后完成的任务走新的回调", async () => {
    const { queue, uploads, done } = harness();
    const later: string[] = [];
    queue.setOnDone((item) => later.push(item.id));
    queue.add([file("a.png")], null);
    uploads[0]!.resolve(asset("7"));
    await tick();
    expect(later).toEqual(["i7"]);
    expect(done).toEqual([]);
  });
});
