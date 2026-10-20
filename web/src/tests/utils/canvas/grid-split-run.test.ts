import { describe, expect, test } from "bun:test";

import type { CanvasNodeData } from "@/types";
import {
  GRID_UPLOAD_CONCURRENCY,
  runGridSplit,
  type GridSplitDeps,
} from "@/utils/canvas/grid-split-run";
import type { UploadOutcome } from "@/utils/canvas/upload-runner";

const IMAGE = {} as CanvasImageSource;

/** 假的外部依赖：记录谁被切、谁被上传、节点被写了什么 */
function fakeDeps(over: Partial<GridSplitDeps> = {}) {
  const cut: Array<[number, number]> = [];
  const uploaded: Array<{ id: string; name: string }> = [];
  const patches: Array<{ id: string; patch: Partial<CanvasNodeData> }> = [];
  const released: string[] = [];
  let closed = 0;
  let inFlight = 0;
  let peak = 0;
  const deps: GridSplitDeps = {
    load: async () => ({ image: IMAGE, width: 1000, height: 600, close: () => void closed++ }),
    cut: async (_image, tile) => {
      cut.push([tile.row, tile.col]);
      return new Blob(["png"], { type: "image/png" });
    },
    upload: async (id, file) => {
      inFlight++;
      peak = Math.max(peak, inFlight);
      // 让出一轮，给并发的其它格机会也进来
      await Promise.resolve();
      await Promise.resolve();
      inFlight--;
      uploaded.push({ id, name: file.name });
      return "done";
    },
    isActive: () => true,
    patch: (id, patch) => void patches.push({ id, patch }),
    release: (id) => void released.push(id),
    ...over,
  };
  return { deps, cut, uploaded, patches, released, closed: () => closed, peak: () => peak };
}

const ids = (n: number) => Array.from({ length: n }, (_, i) => `n${i}`);

describe("runGridSplit 读图、切图、上传", () => {
  test("每格切一次、传一次，文件是 grid-行-列.png 的 PNG", async () => {
    const fake = fakeDeps();
    const result = await runGridSplit(
      { src: "/files/a.png", xs: [0.5], ys: [0.5], nodeIds: ids(4) },
      fake.deps,
    );
    expect(result).toEqual({ status: "done", failed: 0 });
    expect(fake.cut.slice().sort()).toEqual([
      [0, 0],
      [0, 1],
      [1, 0],
      [1, 1],
    ]);
    expect(fake.uploaded.map((item) => item.name).sort()).toEqual([
      "grid-1-1.png",
      "grid-1-2.png",
      "grid-2-1.png",
      "grid-2-2.png",
    ]);
    // 文件和节点一一对应：第 i 个节点拿到第 i 格
    expect(fake.uploaded.find((item) => item.id === "n1")?.name).toBe("grid-1-2.png");
    expect(fake.uploaded.find((item) => item.id === "n2")?.name).toBe("grid-2-1.png");
  });

  test("上传并发不超过上限", async () => {
    const fake = fakeDeps();
    await runGridSplit(
      { src: "/a.png", xs: [0.2, 0.4, 0.6, 0.8], ys: [0.2, 0.4, 0.6, 0.8], nodeIds: ids(25) },
      fake.deps,
    );
    expect(fake.uploaded).toHaveLength(25);
    expect(fake.peak()).toBeLessThanOrEqual(GRID_UPLOAD_CONCURRENCY);
    expect(fake.peak()).toBeGreaterThan(1);
  });

  test("按真实像素回写每个节点的画幅比", async () => {
    const fake = fakeDeps();
    await runGridSplit({ src: "/a.png", xs: [0.25], ys: [], nodeIds: ids(2) }, fake.deps);
    const aspects = fake.patches
      .filter((item) => item.patch.mediaAspect !== undefined)
      .sort((a, b) => a.id.localeCompare(b.id))
      .map((item) => item.patch.mediaAspect);
    // 1000×600 切成 250 和 750 宽
    expect(aspects[0]).toBeCloseTo(250 / 600);
    expect(aspects[1]).toBeCloseTo(750 / 600);
  });

  test("读图失败：返回原因，不切也不传，节点交给调用方撤掉", async () => {
    const fake = fakeDeps({
      load: async () => {
        throw new Error("无法读取原图：服务器返回 404");
      },
    });
    const result = await runGridSplit(
      { src: "/a.png", xs: [0.5], ys: [], nodeIds: ids(2) },
      fake.deps,
    );
    expect(result.status).toBe("unreadable");
    if (result.status === "unreadable")
      expect(result.error.message).toBe("无法读取原图：服务器返回 404");
    expect(fake.cut).toHaveLength(0);
    expect(fake.uploaded).toHaveLength(0);
  });

  test("某格上传失败：只记数，其它格照常传完", async () => {
    const fake = fakeDeps({
      upload: async (id): Promise<UploadOutcome> => (id === "n1" ? "failed" : "done"),
    });
    const result = await runGridSplit(
      { src: "/a.png", xs: [0.5], ys: [0.5], nodeIds: ids(4) },
      fake.deps,
    );
    expect(result).toEqual({ status: "done", failed: 1 });
    expect(fake.cut).toHaveLength(4);
  });

  test("上传被取消不算失败", async () => {
    const fake = fakeDeps({ upload: async () => "aborted" });
    const result = await runGridSplit(
      { src: "/a.png", xs: [0.5], ys: [], nodeIds: ids(2) },
      fake.deps,
    );
    expect(result).toEqual({ status: "done", failed: 0 });
  });

  test("某格切图失败：节点标成失败并释放占位，计入失败数，不影响别的格", async () => {
    const fake = fakeDeps({
      cut: async (_image, tile) => {
        if (tile.col === 1) throw new Error("切图失败：图片导出失败");
        return new Blob(["png"]);
      },
    });
    const result = await runGridSplit(
      { src: "/a.png", xs: [0.5], ys: [], nodeIds: ids(2) },
      fake.deps,
    );
    expect(result).toEqual({ status: "done", failed: 1 });
    expect(fake.uploaded.map((item) => item.id)).toEqual(["n0"]);
    expect(fake.released).toEqual(["n1"]);
    const failedPatch = fake.patches.find(
      (item) => item.id === "n1" && item.patch.status === "error",
    );
    expect(failedPatch?.patch.error).toBe("切图失败：图片导出失败");
    expect(failedPatch?.patch.uploadProgress).toBeUndefined();
  });

  test("用户中途删掉的节点（占位已清）跳过，不切也不传", async () => {
    const fake = fakeDeps({ isActive: (id) => id !== "n1" });
    const result = await runGridSplit(
      { src: "/a.png", xs: [0.5], ys: [], nodeIds: ids(2) },
      fake.deps,
    );
    expect(result).toEqual({ status: "done", failed: 0 });
    expect(fake.cut).toEqual([[0, 0]]);
    expect(fake.uploaded.map((item) => item.id)).toEqual(["n0"]);
  });

  test("读完原图后释放它，失败时也一样", async () => {
    const ok = fakeDeps();
    await runGridSplit({ src: "/a.png", xs: [0.5], ys: [], nodeIds: ids(2) }, ok.deps);
    expect(ok.closed()).toBe(1);

    const bad = fakeDeps({
      cut: async () => {
        throw new Error("切图失败");
      },
    });
    await runGridSplit({ src: "/a.png", xs: [0.5], ys: [], nodeIds: ids(2) }, bad.deps);
    expect(bad.closed()).toBe(1);
  });
});
