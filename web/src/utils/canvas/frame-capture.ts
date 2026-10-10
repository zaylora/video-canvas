/**
 * 从视频里截帧（视频节点「截取帧」的核心）：一个隐藏的 video 加一块 canvas，全在浏览器里做，后端不参与。
 * 素材地址是 /files/，对象存储下会 302 到跨域地址，所以 video 要带 crossOrigin，
 * 存储也要允许跨域读取，否则画布被污染、导不出图片（本地存储同源，不受影响）。
 * 和 utils/showcase/poster.ts 的区别：那边只能截本地 File 的一帧；这里读的是素材地址，
 * 而且一个 video 可以反复定位，截多帧、画胶片缩略图都共用同一次加载。
 */

/** 每一步（加载、定位）最多等多久（毫秒）：视频解不出来时别让界面一直转 */
const STEP_TIMEOUT_MS = 8000;

/** 尾帧比时长往前取这么多秒：正好等于时长时多数浏览器取不到画面 */
const LAST_FRAME_GAP = 0.03;

/** 当前位置离目标这么近就算已经在那儿，不用再定位（也不会再有 seeked 事件） */
const SEEK_EPSILON = 0.001;

/** 整帧导出时的最长边上限（像素）：4K 以上的视频不原样导出，免得图太大 */
export const FULL_FRAME_MAX_EDGE = 4096;

/** 失败的几种原因，界面按它给提示 */
export type FrameCaptureCode = "cors" | "unreadable" | "timeout" | "unsupported";

const MESSAGES: Record<FrameCaptureCode, string> = {
  cors: "存储没有开启跨域读取，没法截取画面，请联系管理员在存储桶配置 CORS",
  unreadable: "读不出视频画面：视频格式不支持，或存储没有开启跨域读取",
  timeout: "截取画面超时，请稍后重试",
  unsupported: "当前浏览器不支持截取画面",
};

/** 截帧失败：code 是原因，message 已经是写给用户的话 */
export class FrameCaptureError extends Error {
  readonly code: FrameCaptureCode;

  constructor(code: FrameCaptureCode, message: string = MESSAGES[code]) {
    super(message);
    this.name = "FrameCaptureError";
    this.code = code;
  }
}

/** 尾帧的时刻（秒）：时长往前一点点，时长太短时就是 0 */
export const lastFrameTime = (duration: number): number => Math.max(duration - LAST_FRAME_GAP, 0);

/** 把时刻限制在 0 到尾帧之间 */
export const clampFrameTime = (time: number, duration: number): number =>
  Math.min(Math.max(time, 0), lastFrameTime(duration));

/** 时间显示成 mm:ss，不满一秒向下取整 */
export function formatFrameTime(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const minutes = String(Math.floor(total / 60)).padStart(2, "0");
  return `${minutes}:${String(total % 60).padStart(2, "0")}`;
}

/** 要截的那一帧：首帧、尾帧，或者某个时刻（秒） */
export type FrameSpec = "first" | "last" | number;

/**
 * 截出来的图片文件名：视频名加帧的叫法。冒号等不能出现在文件名里的字符换成下划线，
 * 自定义帧的时刻写成 00-04。节点标题由上传流程按文件名取，所以名字直接决定节点叫什么。
 */
export function frameFileName(videoLabel: string, spec: FrameSpec): string {
  const safe = videoLabel.replace(/[\\/:*?"<>|]/g, "_");
  const tail =
    spec === "first" ? "首帧" : spec === "last" ? "尾帧" : formatFrameTime(spec).replace(":", "-");
  return `${safe}-${tail}.png`;
}

/** 按最长边封顶等比缩小，不放大 */
export function scaledSize(
  width: number,
  height: number,
  maxEdge: number,
): { width: number; height: number } {
  const scale = Math.min(1, maxEdge / Math.max(width, height));
  return {
    width: Math.max(1, Math.round(width * scale)),
    height: Math.max(1, Math.round(height * scale)),
  };
}

/** 匹配「可选的 http(s) 域名 + /files/ 路径」，和 media-lod 里的一致 */
const FILES_URL = /^(?:https?:\/\/[^/?#]+)?\/files\//i;

/**
 * 截帧专用的视频地址：后端 /files/ 地址追加 cap=1，和画布里播放用的请求区分开。
 * 为什么：同一地址如果先被不带 crossOrigin 的播放器加载过，浏览器可能把没有跨域头的缓存
 * 拿来给带 crossOrigin 的请求用，结果截帧失败。外链、blob 地址可能带签名，不能乱加参数。
 */
export function captureUrl(src: string): string {
  if (!FILES_URL.test(src)) return src;
  const hashAt = src.indexOf("#");
  const base = hashAt >= 0 ? src.slice(0, hashAt) : src;
  const hash = hashAt >= 0 ? src.slice(hashAt) : "";
  return `${base}${base.includes("?") ? "&" : "?"}cap=1${hash}`;
}

/** 截帧用到的 video，真实的 HTMLVideoElement 满足它，测试里可以换成假的 */
export type FrameVideo = {
  crossOrigin: string | null;
  muted: boolean;
  preload: string;
  playsInline: boolean;
  src: string;
  currentTime: number;
  readonly duration: number;
  readonly videoWidth: number;
  readonly videoHeight: number;
  readyState: number;
  readonly paused: boolean;
  readonly ended: boolean;
  play: () => Promise<void>;
  pause: () => void;
  addEventListener: (type: string, listener: () => void) => void;
  removeEventListener: (type: string, listener: () => void) => void;
  removeAttribute: (name: string) => void;
  load: () => void;
};

/** 截帧用到的 canvas，同上 */
export type FrameCanvas = {
  width: number;
  height: number;
  getContext: (type: "2d") => {
    drawImage: (image: CanvasImageSource, dx: number, dy: number, dw: number, dh: number) => void;
  } | null;
  toBlob: (callback: (blob: Blob | null) => void, type?: string, quality?: number) => void;
};

/** 创建 video / canvas 的方式，默认走 document；测试里注入假的 */
export type FrameDeps = {
  createVideo: () => FrameVideo;
  createCanvas: () => FrameCanvas;
};

const defaultDeps: FrameDeps = {
  createVideo: () => document.createElement("video") as unknown as FrameVideo,
  createCanvas: () => document.createElement("canvas") as unknown as FrameCanvas,
};

/** 导出一帧的选项 */
export type GrabOptions = {
  /** 导出文件名 */
  fileName: string;
  /** 最长边上限（像素）；默认 FULL_FRAME_MAX_EDGE */
  maxEdge?: number;
  /** 图片格式；默认 PNG（保真，后面还要当首帧用），胶片缩略图用 JPEG */
  type?: "image/png" | "image/jpeg";
  /** JPEG 质量 0 到 1 */
  quality?: number;
};

/** 打开视频后得到的截帧器 */
export type FrameReader = {
  /** 视频时长（秒） */
  duration: number;
  /** 视频画面的原始尺寸 */
  size: { width: number; height: number };
  /** 定位到某个时刻并等画面出来；和 grab 排同一个队列。时刻限制在 0 到尾帧之间 */
  seek: (time: number) => Promise<void>;
  /** 当前播放位置（秒） */
  time: () => number;
  /** 把当前画面画满目标画布（预览用，不编码，播放时每一帧调用也不贵） */
  drawTo: (canvas: FrameCanvas) => void;
  /** 开始播放（静音），需要用户手势触发 */
  play: () => Promise<void>;
  /** 暂停播放 */
  pause: () => void;
  /** 此刻是否在播放 */
  playing: () => boolean;
  /** 截一帧。多次调用会排队，一个 video 同一时间只处理一个 */
  grab: (spec: FrameSpec, options: GrabOptions) => Promise<File>;
  /** 释放视频资源；用完必须调用 */
  dispose: () => void;
};

/**
 * 等某个事件发生。出错事件 / 超时都会让它失败；不管怎么结束都会把监听摘干净。
 */
function waitFor(
  video: FrameVideo,
  done: string,
  timeoutMs: number,
  onError: () => FrameCaptureError = () => new FrameCaptureError("unreadable"),
): { promise: Promise<void>; cancel: () => void } {
  let cleanup = () => {};
  const promise = new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new FrameCaptureError("timeout")), timeoutMs);
    const succeed = () => resolve();
    const fail = () => reject(onError());
    video.addEventListener(done, succeed);
    video.addEventListener("error", fail);
    cleanup = () => {
      clearTimeout(timer);
      video.removeEventListener(done, succeed);
      video.removeEventListener("error", fail);
    };
  }).finally(() => cleanup());
  return { promise, cancel: () => cleanup() };
}

/**
 * 打开一个视频准备截帧：等第一帧画面出来，读出时长和尺寸。
 * @param src 素材地址（一般是节点的 /files/ 地址）
 * @param deps 创建 video / canvas 的方式，测试时注入
 * @param options.timeoutMs 每一步最多等多久（毫秒）
 * @throws FrameCaptureError 读不出画面、超时、时长未知
 */
export async function openFrameReader(
  src: string,
  deps: FrameDeps = defaultDeps,
  options: { timeoutMs?: number } = {},
): Promise<FrameReader> {
  const timeoutMs = options.timeoutMs ?? STEP_TIMEOUT_MS;
  const video = deps.createVideo();
  video.crossOrigin = "anonymous";
  video.muted = true;
  video.preload = "auto";
  video.playsInline = true;

  let disposed = false;
  const dispose = () => {
    if (disposed) return;
    disposed = true;
    video.removeAttribute("src");
    video.load();
  };

  const loaded = waitFor(video, "loadeddata", timeoutMs);
  video.src = captureUrl(src);
  try {
    await loaded.promise;
  } catch (error) {
    dispose();
    throw error;
  }

  const duration = video.duration;
  const size = { width: video.videoWidth, height: video.videoHeight };
  if (!Number.isFinite(duration) || duration <= 0 || size.width <= 0 || size.height <= 0) {
    dispose();
    throw new FrameCaptureError("unreadable");
  }

  const seekTo = async (time: number) => {
    if (Math.abs(video.currentTime - time) < SEEK_EPSILON) return;
    // 先挂监听再改 currentTime：seeked 可能在赋值后的同一个微任务里就来了
    const seeked = waitFor(video, "seeked", timeoutMs);
    video.currentTime = time;
    await seeked.promise;
  };

  // 真实环境里 video 就是 HTMLVideoElement；类型上只暴露了截帧要用的那几项，这里还原给 drawImage
  const frame = video as unknown as CanvasImageSource;

  const draw = (width: number, height: number): FrameCanvas => {
    const canvas = deps.createCanvas();
    canvas.width = width;
    canvas.height = height;
    const context = canvas.getContext("2d");
    if (!context) throw new FrameCaptureError("unsupported");
    context.drawImage(frame, 0, 0, width, height);
    return canvas;
  };

  const encode = (canvas: FrameCanvas, type: string, quality?: number) =>
    new Promise<Blob>((resolve, reject) => {
      try {
        canvas.toBlob(
          (blob) => (blob ? resolve(blob) : reject(new FrameCaptureError("unreadable"))),
          type,
          quality,
        );
      } catch (error) {
        // 画布被污染（存储没给跨域头）时，导出会直接抛 SecurityError
        const tainted = error instanceof DOMException && error.name === "SecurityError";
        reject(new FrameCaptureError(tainted ? "cors" : "unreadable"));
      }
    });

  const run = async (spec: FrameSpec, grab: GrabOptions): Promise<File> => {
    alive();
    const wanted =
      spec === "first"
        ? 0
        : spec === "last"
          ? lastFrameTime(duration)
          : clampFrameTime(spec, duration);
    await seekTo(wanted);
    const target = scaledSize(size.width, size.height, grab.maxEdge ?? FULL_FRAME_MAX_EDGE);
    const type = grab.type ?? "image/png";
    const blob = await encode(draw(target.width, target.height), type, grab.quality);
    return new File([blob], grab.fileName, { type: blob.type || type });
  };

  const alive = () => {
    if (disposed) throw new FrameCaptureError("unreadable", "视频已经关闭，没法再截取");
  };

  // 排队：前一个无论成败，后一个都接着做
  let queue: Promise<unknown> = Promise.resolve();
  const enqueue = <T>(task: () => Promise<T>): Promise<T> => {
    const next = queue.then(task);
    queue = next.catch(() => undefined);
    return next;
  };

  return {
    duration,
    size,
    seek: (time) =>
      enqueue(async () => {
        alive();
        await seekTo(clampFrameTime(time, duration));
      }),
    time: () => video.currentTime,
    drawTo: (canvas) => {
      if (disposed) return;
      canvas.getContext("2d")?.drawImage(frame, 0, 0, canvas.width, canvas.height);
    },
    play: () => {
      alive();
      return video.play();
    },
    pause: () => video.pause(),
    playing: () => !video.paused && !video.ended,
    grab: (spec, grab) => enqueue(() => run(spec, grab)),
    dispose,
  };
}
