import type { CameraRender, DirectorExportEngine } from "./export";
import { DIRECTOR_FPS } from "./scene";
import { loadFigureModels } from "./figure";
import { createShotRenderer } from "./render";

/** Mediabunny 只在导出时才加载，不进画布首屏的包 */
const loadMediabunny = () => import("mediabunny");

/** 不能导出时给用户看的原因 */
export const UNSUPPORTED_EXPORT_MESSAGE =
  "当前浏览器不支持 H.264 视频编码，请用新版 Chrome / Edge 导出";

/** 当前浏览器能不能把 1080p 画面编码成 H.264（导出按钮据此禁用并说明原因） */
export async function canExportVideo(): Promise<boolean> {
  if (typeof VideoEncoder === "undefined") return false;
  const { canEncodeVideo } = await loadMediabunny();
  return canEncodeVideo("avc", { width: 1920, height: 1080 }).catch(() => false);
}

/**
 * 真实导出引擎：three.js 离屏渲染每一帧，Mediabunny 的 CanvasSource 逐帧编码成 H.264 MP4。
 * 每个机位开一个渲染器和一个输出，结束或取消时释放。
 */
export function createExportEngine(): DirectorExportEngine {
  return {
    async open(scene, cameraId, size): Promise<CameraRender> {
      const { BufferTarget, CanvasSource, Mp4OutputFormat, Output, QUALITY_HIGH } =
        await loadMediabunny();
      // 先把场景里用到的人台模型读好：之后每一帧都是同一个人物，导出中途不会换形体；读失败就用简化形体
      await loadFigureModels(scene.characters.map((c) => c.model ?? "male"));
      const shot = createShotRenderer();
      shot.setSize(size.width, size.height);
      const target = new BufferTarget();
      const output = new Output({
        format: new Mp4OutputFormat({ fastStart: "in-memory" }),
        target,
      });
      const source = new CanvasSource(shot.canvas, {
        codec: "avc",
        quality: QUALITY_HIGH,
        keyFrameInterval: 1,
      });
      output.addVideoTrack(source, { frameRate: DIRECTOR_FPS });
      await output.start();
      let closed = false;
      const close = () => {
        if (closed) return;
        closed = true;
        shot.dispose();
      };
      return {
        renderFrame: async (t) => {
          if (!shot.render(scene, cameraId, t)) throw new Error("机位不存在");
        },
        encodeFrame: (t, duration) => source.add(t, duration),
        snapshot: () => shot.toBlob("image/png"),
        finish: async () => {
          try {
            await output.finalize();
            if (!target.buffer) throw new Error("编码没有产出数据");
            return new Blob([target.buffer], { type: "video/mp4" });
          } finally {
            close();
          }
        },
        cancel: async () => {
          try {
            await output.cancel();
          } finally {
            close();
          }
        },
      };
    },
  };
}
