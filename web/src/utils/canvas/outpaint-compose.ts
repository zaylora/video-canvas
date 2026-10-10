import type { FrameCanvas } from "./frame-capture";
import { outputSize, type ImageSize, type OutpaintFrame } from "./outpaint";

/**
 * 扩图的读图和拼图（设计稿 docs/design/画布UI设计 6.17）：全在浏览器里做，后端不参与。
 * 读原图用 `fetch`（跨域模式、绕过缓存）取成 blob 再解码，而不是给 img 加 crossOrigin：
 * 节点里的 img 先前不带跨域头加载过同一张图，浏览器缓存里的那份响应没有 CORS 头，
 * 再用带 crossOrigin 的 img 去读会复用它而失败。blob 来源是同源的，拼图时画布也不会被污染。
 * 对象存储仍然必须给素材域名配 CORS，否则 fetch 本身就会被拒；本地存储同源不受影响。
 */

/** 读图最多等多久（毫秒）：解不出来时别让界面一直转 */
const LOAD_TIMEOUT_MS = 15000;

/** 失败的几种原因，界面按它给提示 */
export type OutpaintErrorCode = "cors" | "unreadable" | "timeout" | "unsupported";

const MESSAGES: Record<OutpaintErrorCode, string> = {
  cors: "存储没有开启跨域读取，没法读取原图，请联系管理员在存储桶配置 CORS",
  unreadable: "读不出原图：地址打不开，或图片格式不支持",
  timeout: "读取原图超时，请稍后重试",
  unsupported: "当前浏览器不支持合成图片",
};

/** 扩图读图 / 拼图失败：code 是原因，message 已经是写给用户的话 */
export class OutpaintError extends Error {
  readonly code: OutpaintErrorCode;

  constructor(code: OutpaintErrorCode) {
    super(MESSAGES[code]);
    this.name = "OutpaintError";
    this.code = code;
  }
}

/** 解码后的位图，真实的 ImageBitmap 满足它；用完 close 释放 */
export type OutpaintBitmap = {
  readonly width: number;
  readonly height: number;
  close?: () => void;
};

/** fetch 返回里用到的部分 */
type FetchResponse = { ok: boolean; status: number; blob: () => Promise<Blob> };

/** 网络、解码、画布的来源，默认走浏览器；测试里注入假的 */
export type OutpaintDeps = {
  fetch: (url: string, init: RequestInit) => Promise<FetchResponse>;
  decode: (blob: Blob) => Promise<OutpaintBitmap>;
  createCanvas: () => FrameCanvas;
};

const defaultDeps: OutpaintDeps = {
  fetch: (url, init) => fetch(url, init),
  decode: (blob) => createImageBitmap(blob),
  createCanvas: () => document.createElement("canvas") as unknown as FrameCanvas,
};

/** 读好的原图：位图和它的像素尺寸 */
export type OutpaintImage = { element: OutpaintBitmap; size: ImageSize };

/**
 * 读原图：跨域模式取回来、绕过缓存，解码，读出像素尺寸。
 * fetch 失败时再发一次不要求跨域头的探测请求：探测能通，说明是存储没开跨域读取；
 * 探测也不通才是地址打不开，两种情况给的提示不一样。
 * @param src 素材地址（节点的 /files/ 地址）
 * @throws OutpaintError 读不出、被跨域拒绝、超时
 */
export async function loadOutpaintImage(
  src: string,
  deps: OutpaintDeps = defaultDeps,
  options: { timeoutMs?: number } = {},
): Promise<OutpaintImage> {
  const controller = new AbortController();
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      controller.abort();
      reject(new OutpaintError("timeout"));
    }, options.timeoutMs ?? LOAD_TIMEOUT_MS);
  });

  const read = async (): Promise<OutpaintImage> => {
    let response: FetchResponse;
    try {
      response = await deps.fetch(src, {
        mode: "cors",
        cache: "no-store",
        signal: controller.signal,
      });
    } catch (cause) {
      console.warn("[outpaint] 读原图失败", src, cause);
      const reachable = await deps
        .fetch(src, { mode: "no-cors", cache: "no-store", signal: controller.signal })
        .then(
          () => true,
          () => false,
        );
      throw new OutpaintError(reachable ? "cors" : "unreadable");
    }
    if (!response.ok) {
      console.warn("[outpaint] 读原图失败：服务端返回", response.status, src);
      throw new OutpaintError("unreadable");
    }
    let element: OutpaintBitmap;
    try {
      element = await deps.decode(await response.blob());
    } catch (cause) {
      console.warn("[outpaint] 解码原图失败", src, cause);
      throw new OutpaintError("unreadable");
    }
    const size = { width: element.width, height: element.height };
    if (size.width <= 0 || size.height <= 0) throw new OutpaintError("unreadable");
    return { element, size };
  };

  try {
    return await Promise.race([read(), timeout]);
  } finally {
    clearTimeout(timer);
  }
}

/**
 * 把原图按框的范围拼进一张更大的透明 PNG：画布大小是框的输出尺寸，原图画在对应偏移处，
 * 其余保持透明（不填色），交给模型去补全。最长边超过上限时整体等比缩小，原图也按同一比例缩。
 * @param image 读好的原图
 * @param frame 框（原图像素，原图左上角为原点）
 * @param fileName 导出的文件名
 * @throws OutpaintError 导出失败、浏览器不支持
 */
export async function composeOutpaint(
  image: OutpaintImage,
  frame: OutpaintFrame,
  fileName: string,
  deps: OutpaintDeps = defaultDeps,
): Promise<File> {
  const out = outputSize(frame);
  const canvas = deps.createCanvas();
  canvas.width = out.width;
  canvas.height = out.height;
  const context = canvas.getContext("2d");
  if (!context) throw new OutpaintError("unsupported");
  // 真实环境里 element 就是 ImageBitmap；类型上只暴露了拼图要用的几项，这里还原给 drawImage
  context.drawImage(
    image.element as unknown as CanvasImageSource,
    (0 - frame.x0) * out.scale,
    (0 - frame.y0) * out.scale,
    image.size.width * out.scale,
    image.size.height * out.scale,
  );
  const blob = await new Promise<Blob>((resolve, reject) => {
    try {
      canvas.toBlob(
        (result) => (result ? resolve(result) : reject(new OutpaintError("unreadable"))),
        "image/png",
      );
    } catch (error) {
      // 位图来自同源 blob，一般不会被污染；保险起见仍把 SecurityError 翻译成跨域提示
      const tainted = error instanceof DOMException && error.name === "SecurityError";
      reject(new OutpaintError(tainted ? "cors" : "unreadable"));
    }
  });
  return new File([blob], fileName, { type: "image/png" });
}
