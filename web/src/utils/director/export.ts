import type { DirectorAspect, DirectorScene } from "@/types";

import { DIRECTOR_FPS, frameCount } from "./scene";

/** 导出分辨率：长边 1280 或 1920 */
export type ExportResolution = "720p" | "1080p";

const LONG_SIDE: Record<ExportResolution, number> = { "720p": 1280, "1080p": 1920 };

/** 画幅的宽高比 */
export const ASPECT_RATIO: Record<DirectorAspect, number> = {
  "16:9": 16 / 9,
  "9:16": 9 / 16,
  "1:1": 1,
  "4:3": 4 / 3,
  "21:9": 21 / 9,
};

const even = (n: number) => Math.round(n / 2) * 2;

/** 导出画面尺寸：长边固定，短边按画幅算并取偶数（H.264 要求） */
export function exportSize(
  aspect: DirectorAspect,
  resolution: ExportResolution,
): { width: number; height: number } {
  const long = LONG_SIDE[resolution];
  const ratio = ASPECT_RATIO[aspect];
  return ratio >= 1
    ? { width: long, height: even(long / ratio) }
    : { width: even(long * ratio), height: long };
}

/** 导出文件名：导演台名_机位名.扩展名，文件名里的非法字符换成 - */
export const exportFileName = (nodeLabel: string, cameraName: string, ext: "mp4" | "png") =>
  `${nodeLabel}_${cameraName}`.replace(/[\\/:*?"<>|]/g, "-") + `.${ext}`;

/** 一个机位的渲染与编码会话：画面尺寸在 open 时就定下 */
export type CameraRender = {
  /** 把 t 秒的画面画到离屏画布上（不带网格、标签、控件） */
  renderFrame: (t: number) => Promise<void>;
  /** 把当前画面编码成一帧 */
  encodeFrame: (t: number, duration: number) => Promise<void>;
  /** 当前画面存成 PNG（首帧图） */
  snapshot: () => Promise<Blob>;
  /** 收尾，拿到 MP4 */
  finish: () => Promise<Blob>;
  /** 丢弃已编码的内容 */
  cancel: () => Promise<void>;
};

/** 导出引擎：真实实现是 three.js 离屏渲染 + Mediabunny 编码，测试里注入假的 */
export type DirectorExportEngine = {
  open: (
    scene: DirectorScene,
    cameraId: string,
    size: { width: number; height: number },
  ) => Promise<CameraRender>;
};

export type CameraExportResult =
  | {
      cameraId: string;
      ok: true;
      video: Blob;
      firstFrame?: Blob;
      width: number;
      height: number;
    }
  | { cameraId: string; ok: false; error: string };

const abortError = () => new DOMException("已取消导出", "AbortError");

/**
 * 逐个机位离线导出：按 1/24 秒步进渲染每一帧再编码，不依赖实时播放，时间精确。
 * 一个机位失败只记下原因、接着导下一个；取消时丢弃当前机位已编码的内容并抛 AbortError。
 */
export async function exportCameras(
  input: {
    scene: DirectorScene;
    cameraIds: string[];
    resolution: ExportResolution;
    withFirstFrame: boolean;
    signal?: AbortSignal;
    onProgress?: (cameraId: string, done: number, total: number) => void;
  },
  engine: DirectorExportEngine,
): Promise<CameraExportResult[]> {
  const { scene, cameraIds, resolution, withFirstFrame, signal, onProgress } = input;
  const total = frameCount(scene.duration);
  const step = 1 / DIRECTOR_FPS;
  const results: CameraExportResult[] = [];

  for (const cameraId of cameraIds) {
    if (signal?.aborted) throw abortError();
    const camera = scene.cameras.find((c) => c.id === cameraId);
    if (!camera) {
      results.push({ cameraId, ok: false, error: "机位不存在" });
      continue;
    }
    const size = exportSize(camera.aspect, resolution);
    let render: CameraRender | undefined;
    try {
      render = await engine.open(scene, cameraId, size);
      let firstFrame: Blob | undefined;
      for (let i = 0; i < total; i++) {
        if (signal?.aborted) throw abortError();
        const t = i * step;
        await render.renderFrame(t);
        if (i === 0 && withFirstFrame) firstFrame = await render.snapshot();
        await render.encodeFrame(t, step);
        onProgress?.(cameraId, i + 1, total);
      }
      if (signal?.aborted) throw abortError();
      const video = await render.finish();
      results.push({ cameraId, ok: true, video, firstFrame, ...size });
    } catch (error) {
      await render?.cancel().catch(() => {});
      if ((error as Error)?.name === "AbortError" || signal?.aborted) throw abortError();
      results.push({
        cameraId,
        ok: false,
        error: error instanceof Error ? error.message : "导出失败",
      });
    }
  }
  return results;
}
