import type { Input } from "mediabunny";

import { TrimError, type TrimEncodeArgs, type TrimEngine, type TrimProbe } from "./trim-run";

/** 当前浏览器能不能做视频修剪：要有 WebCodecs 的视频编解码 */
export function supportsTrim(): boolean {
  return typeof VideoEncoder !== "undefined" && typeof VideoDecoder !== "undefined";
}

/** 不支持时给用户看的话，和 TrimError("unsupported") 同一句 */
export const UNSUPPORTED_TRIM_MESSAGE = new TrimError("unsupported").message;

/** Mediabunny 只在真正用到时才加载，不进画布首屏的包 */
const loadMediabunny = () => import("mediabunny");

/** 打开一个视频：带跨域读取，读取地址交给 Mediabunny 自己分段请求 */
async function openInput(src: string): Promise<Input> {
  const { ALL_FORMATS, Input, UrlSource } = await loadMediabunny();
  return new Input({
    formats: ALL_FORMATS,
    source: new UrlSource(src, { requestInit: { mode: "cors" } }),
  });
}

/** 中止信号触发时抛 AbortError，和运行器的判断对上 */
function throwIfAborted(signal: AbortSignal) {
  if (signal.aborted) throw new DOMException("已中止", "AbortError");
}

/**
 * 读源视频的时长、画幅、有没有音轨。
 * 读不到画面轨道、或解不了码，抛对应的 TrimError。
 */
async function probe(src: string, signal: AbortSignal): Promise<TrimProbe> {
  if (!supportsTrim()) throw new TrimError("unsupported");
  const input = await openInput(src);
  try {
    const video = await input.getPrimaryVideoTrack();
    throwIfAborted(signal);
    if (!video) throw new TrimError("no-video");
    if (!(await video.canDecode())) throw new TrimError("undecodable");
    const [duration, audio] = await Promise.all([
      input.computeDuration(),
      input.getPrimaryAudioTrack(),
    ]);
    throwIfAborted(signal);
    return {
      duration,
      hasAudio: audio !== null,
      width: video.displayWidth,
      height: video.displayHeight,
    };
  } finally {
    input.dispose();
  }
}

/**
 * 按时刻取胶片条缩略图，返回 JPEG 的 object URL；某一格取不到就是 null，不让整条失败。
 * 缩略图只是示意，所以用较小的高度和有损压缩，免得 30 格占满内存。
 */
async function thumbnails(
  src: string,
  times: number[],
  height: number,
  signal: AbortSignal,
): Promise<(string | null)[]> {
  const { CanvasSink } = await loadMediabunny();
  const input = await openInput(src);
  try {
    const video = await input.getPrimaryVideoTrack();
    if (!video || !(await video.canDecode())) return times.map(() => null);
    const sink = new CanvasSink(video, { height, fit: "contain" });
    const out: (string | null)[] = [];
    for await (const wrapped of sink.canvasesAtTimestamps(times)) {
      throwIfAborted(signal);
      out.push(wrapped ? await canvasToUrl(wrapped.canvas) : null);
    }
    return out;
  } finally {
    input.dispose();
  }
}

/** 画布转成 JPEG 的 object URL；OffscreenCanvas 和普通 canvas 的写法不同 */
async function canvasToUrl(canvas: HTMLCanvasElement | OffscreenCanvas): Promise<string | null> {
  const blob =
    "convertToBlob" in canvas
      ? await canvas.convertToBlob({ type: "image/jpeg", quality: 0.7 })
      : await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", 0.7));
  return blob ? URL.createObjectURL(blob) : null;
}

/**
 * 编码一段选区成 MP4。trim.start 不为 0 时 Mediabunny 一定重新编码，
 * 所以第一版不区分「可以直拷」的情况；去音轨用 audio.discard。
 * 取消靠 signal：触发后让 Conversion 取消，抛 AbortError 给运行器收尾。
 */
async function encode(args: TrimEncodeArgs): Promise<Blob> {
  if (!supportsTrim()) throw new TrimError("unsupported");
  const { BufferTarget, Conversion, ConversionCanceledError, Mp4OutputFormat, Output } =
    await loadMediabunny();
  const input = await openInput(args.src);
  const target = new BufferTarget();
  const output = new Output({ format: new Mp4OutputFormat(), target });
  try {
    const conversion = await Conversion.init({
      input,
      output,
      trim: { start: args.start, end: args.end },
      video: { discard: false },
      audio: { discard: args.discardAudio },
    });
    if (!conversion.isValid) {
      const reasons = conversion.discardedTracks.map((item) => item.reason);
      if (reasons.includes("undecodable_source_codec")) throw new TrimError("undecodable");
      if (reasons.includes("no_encodable_target_codec")) throw new TrimError("unsupported");
      throw new TrimError("no-video");
    }
    conversion.onProgress = (ratio) => args.onProgress(ratio);
    const onAbort = () => void conversion.cancel();
    args.signal.addEventListener("abort", onAbort, { once: true });
    try {
      throwIfAborted(args.signal);
      await conversion.execute();
    } finally {
      args.signal.removeEventListener("abort", onAbort);
    }
    throwIfAborted(args.signal);
    if (!target.buffer) throw new Error("编码没有产出数据");
    return new Blob([target.buffer], { type: "video/mp4" });
  } catch (error) {
    if (error instanceof ConversionCanceledError) {
      throw new DOMException("已中止", "AbortError");
    }
    throw error;
  } finally {
    input.dispose();
  }
}

/** 默认引擎：Mediabunny + WebCodecs */
export const trimEngine: TrimEngine = { probe, thumbnails, encode };
