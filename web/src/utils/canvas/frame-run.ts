import { describeTrimError, TrimError } from "./trim-run";
import type { FrameSpec } from "./frame";

/** 导出一帧的选项 */
export type FrameOptions = {
  /** 最长边上限（像素） */
  maxEdge: number;
  type: "image/png" | "image/jpeg";
};

/** 整帧导出时的最长边上限（像素）：4K 以上的视频不原样导出，免得图太大 */
export const FULL_FRAME_MAX_EDGE = 4096;

/** 取帧引擎：按「首帧、尾帧、某个时刻」解码出一张图。真实实现用 Mediabunny，测试里换成假的 */
export type FrameEngine = {
  frameAt: (
    src: string,
    spec: FrameSpec,
    options: FrameOptions,
    signal: AbortSignal,
  ) => Promise<Blob>;
};

/** 一次取帧要做的事 */
export type FrameJob = {
  /** 源视频地址（稳定的 /files/ 路径） */
  src: string;
  spec: FrameSpec;
  /** 产出文件名 */
  fileName: string;
};

const isAbort = (error: unknown) => error instanceof DOMException && error.name === "AbortError";

/**
 * 取一帧，包成 PNG 文件。读视频的地址和画布播放一样，两边带同样的跨域头，共用浏览器缓存。
 * 失败时抛 Error，message 已经是写给用户的话；中止时原样抛 AbortError，调用方不该提示。
 * @param engine 实际取帧的引擎，默认走 Mediabunny；测试时注入假的
 */
export async function grabFrame(
  engine: FrameEngine,
  job: FrameJob,
  signal: AbortSignal = new AbortController().signal,
): Promise<File> {
  try {
    const blob = await engine.frameAt(
      job.src,
      job.spec,
      { maxEdge: FULL_FRAME_MAX_EDGE, type: "image/png" },
      signal,
    );
    return new File([blob], job.fileName, { type: blob.type || "image/png" });
  } catch (error) {
    if (isAbort(error) || signal.aborted) throw error;
    if (error instanceof TrimError) throw error;
    const message = describeTrimError(error);
    // 取帧不是「修剪」，通用兜底换成截取的说法
    throw new Error(message === "修剪失败，请稍后重试" ? "截取失败，请稍后重试" : message);
  }
}
