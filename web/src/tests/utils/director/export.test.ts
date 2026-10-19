import { describe, expect, test } from "bun:test";

import type { DirectorScene } from "@/types";
import {
  type CameraRender,
  type DirectorExportEngine,
  exportCameras,
  exportFileName,
  exportSize,
} from "@/utils/director/export";
import { addCamera, createDefaultScene } from "@/utils/director/scene";

const scene = (): DirectorScene => {
  let n = 0;
  const base = createDefaultScene(() => `id${++n}`);
  return addCamera(base, () => "cam2").scene;
};

/** 假引擎：记下每个机位渲染了哪些时刻；fail 里的机位在第 failAt 帧抛错 */
function fakeEngine(opts: { fail?: string; failAt?: number } = {}) {
  const log: Record<string, number[]> = {};
  const cancelled: string[] = [];
  const opened: { cameraId: string; width: number; height: number }[] = [];
  const engine: DirectorExportEngine = {
    async open(_scene, cameraId, size) {
      opened.push({ cameraId, ...size });
      log[cameraId] = [];
      const render: CameraRender = {
        async renderFrame(t) {
          if (cameraId === opts.fail && log[cameraId].length === (opts.failAt ?? 0)) {
            throw new Error("VideoEncoder 配置失败");
          }
          log[cameraId].push(t);
        },
        async encodeFrame() {},
        async snapshot() {
          return new Blob(["png"], { type: "image/png" });
        },
        async finish() {
          return new Blob(["mp4"], { type: "video/mp4" });
        },
        async cancel() {
          cancelled.push(cameraId);
        },
      };
      return render;
    },
  };
  return { engine, log, cancelled, opened };
}

describe("导出尺寸", () => {
  test("长边 1280 / 1920，按画幅算短边，取偶数", () => {
    expect(exportSize("16:9", "720p")).toEqual({ width: 1280, height: 720 });
    expect(exportSize("16:9", "1080p")).toEqual({ width: 1920, height: 1080 });
    expect(exportSize("9:16", "720p")).toEqual({ width: 720, height: 1280 });
    expect(exportSize("1:1", "720p")).toEqual({ width: 1280, height: 1280 });
    expect(exportSize("21:9", "720p")).toEqual({ width: 1280, height: 548 });
  });
});

describe("文件名", () => {
  test("导演台名_机位名，去掉文件名非法字符", () => {
    expect(exportFileName("导演台1", "机位1", "mp4")).toBe("导演台1_机位1.mp4");
    expect(exportFileName("a/b", "c:d", "png")).toBe("a-b_c-d.png");
  });
});

describe("逐帧导出", () => {
  test("每个机位按 24fps 渲染整段，时间不依赖实时播放", async () => {
    const { engine, log, opened } = fakeEngine();
    const results = await exportCameras(
      { scene: scene(), cameraIds: ["id2", "cam2"], resolution: "720p", withFirstFrame: true },
      engine,
    );
    expect(results.map((r) => r.ok)).toEqual([true, true]);
    expect(log.id2).toHaveLength(120);
    expect(log.id2[1]).toBeCloseTo(1 / 24, 9);
    expect(log.id2.at(-1)).toBeCloseTo(119 / 24, 9);
    expect(opened[0]).toEqual({ cameraId: "id2", width: 1280, height: 720 });
  });

  test("勾选首帧图时带回 PNG，不勾不带", async () => {
    const withFrame = await exportCameras(
      { scene: scene(), cameraIds: ["id2"], resolution: "720p", withFirstFrame: true },
      fakeEngine().engine,
    );
    const without = await exportCameras(
      { scene: scene(), cameraIds: ["id2"], resolution: "720p", withFirstFrame: false },
      fakeEngine().engine,
    );
    expect(withFrame[0].ok && withFrame[0].firstFrame?.type).toBe("image/png");
    expect(without[0].ok && without[0].firstFrame).toBeUndefined();
  });

  test("进度按帧回报", async () => {
    const progress: [string, number, number][] = [];
    await exportCameras(
      {
        scene: scene(),
        cameraIds: ["id2"],
        resolution: "720p",
        withFirstFrame: false,
        onProgress: (id, done, total) => progress.push([id, done, total]),
      },
      fakeEngine().engine,
    );
    expect(progress[0]).toEqual(["id2", 1, 120]);
    expect(progress.at(-1)).toEqual(["id2", 120, 120]);
  });

  test("一个机位失败不影响其他机位，失败的给出原因", async () => {
    const { engine, cancelled } = fakeEngine({ fail: "id2", failAt: 30 });
    const results = await exportCameras(
      { scene: scene(), cameraIds: ["id2", "cam2"], resolution: "720p", withFirstFrame: false },
      engine,
    );
    expect(results[0]).toMatchObject({ cameraId: "id2", ok: false });
    expect(!results[0].ok && results[0].error).toContain("VideoEncoder");
    expect(results[1]).toMatchObject({ cameraId: "cam2", ok: true });
    expect(cancelled).toEqual(["id2"]);
  });

  test("取消时丢弃当前机位并抛 AbortError", async () => {
    const controller = new AbortController();
    const { engine, cancelled } = fakeEngine();
    const run = exportCameras(
      {
        scene: scene(),
        cameraIds: ["id2", "cam2"],
        resolution: "720p",
        withFirstFrame: false,
        signal: controller.signal,
        onProgress: (_id, done) => done === 10 && controller.abort(),
      },
      engine,
    );
    await expect(run).rejects.toMatchObject({ name: "AbortError" });
    expect(cancelled).toEqual(["id2"]);
  });

  test("不存在的机位记为失败", async () => {
    const results = await exportCameras(
      { scene: scene(), cameraIds: ["nope"], resolution: "720p", withFirstFrame: false },
      fakeEngine().engine,
    );
    expect(results[0]).toMatchObject({ ok: false, cameraId: "nope" });
  });
});
