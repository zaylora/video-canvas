import { frameHeight, frameWidth, type OriginSize, type OutpaintFrame } from "./outpaint-frame";

/** 合成图最长边最多多少像素：框最大是原图的 3 倍，1920 宽的图会拼出 5760 宽，超出就等比压缩 */
export const COMPOSE_MAX_EDGE = 4096;

/** 合成方案：画布多大，原图贴在哪、多大（都是整数像素） */
export type ComposePlan = {
  width: number;
  height: number;
  imageX: number;
  imageY: number;
  imageW: number;
  imageH: number;
  /** 有没有因为超过 COMPOSE_MAX_EDGE 被压缩过 */
  scaled: boolean;
};

/**
 * 算合成方案：把画布坐标里的框换算成像素。
 * 原图节点在画布上是 origin 这么大，原图真实像素是 natural，两者的比值就是「每个画布单位多少像素」；
 * 框里除了原图都是要新生成的透明区域。
 * @param input.natural 原图真实像素
 * @param input.origin 原图节点在画布上的大小（画布坐标，100% 缩放）
 * @param input.frame 扩图框（相对原图左上角）
 * @param input.maxEdge 最长边上限，默认 COMPOSE_MAX_EDGE
 */
export function planCompose(input: {
  natural: OriginSize;
  origin: OriginSize;
  frame: OutpaintFrame;
  maxEdge?: number;
}): ComposePlan {
  const { natural, origin, frame, maxEdge = COMPOSE_MAX_EDGE } = input;
  const sx = natural.w / origin.w;
  const sy = natural.h / origin.h;
  const fullW = frameWidth(frame) * sx;
  const fullH = frameHeight(frame) * sy;
  const shrink = Math.max(fullW, fullH) > maxEdge ? maxEdge / Math.max(fullW, fullH) : 1;

  const width = Math.max(1, Math.round(fullW * shrink));
  const height = Math.max(1, Math.round(fullH * shrink));
  const imageW = Math.min(width, Math.round(natural.w * shrink));
  const imageH = Math.min(height, Math.round(natural.h * shrink));
  // 四舍五入可能让原图差 1px 贴出画布边，收回来
  // 0 - x 而不是 -x：x 为 0 时 Math.round(-0) 还是 -0
  const imageX = Math.min(Math.round(0 - frame.x0 * sx * shrink), width - imageW);
  const imageY = Math.min(Math.round(0 - frame.y0 * sy * shrink), height - imageH);
  return { width, height, imageX, imageY, imageW, imageH, scaled: shrink < 1 };
}

/** 导出合成图只用到的画布能力；真实的 HTMLCanvasElement 满足它，测试里换成假的 */
export type CompositeCanvas = {
  width: number;
  height: number;
  getContext: (type: "2d") => { drawImage: (...args: never[]) => void } | null;
  toBlob: (callback: (blob: Blob | null) => void, type?: string) => void;
};

const createCanvas = (): CompositeCanvas =>
  document.createElement("canvas") as unknown as CompositeCanvas;

/**
 * 把原图贴到一张透明画布上，导出 PNG。画布初始就是全透明，不用额外清除。
 * @param source 原图（ImageBitmap 等）
 * @param plan planCompose 的结果
 * @param makeCanvas 造画布的函数，测试里注入假的
 * @throws 拿不到绘图上下文或导出失败时抛 Error，文案可直接显示在节点上
 */
export async function renderComposite(
  source: CanvasImageSource,
  plan: ComposePlan,
  makeCanvas: () => CompositeCanvas = createCanvas,
): Promise<Blob> {
  const canvas = makeCanvas();
  canvas.width = plan.width;
  canvas.height = plan.height;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("合成图导出失败：浏览器不支持画布");
  (context.drawImage as (...args: unknown[]) => void)(
    source,
    plan.imageX,
    plan.imageY,
    plan.imageW,
    plan.imageH,
  );
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("合成图导出失败"))),
      "image/png",
    );
  });
}

/**
 * 读原图像素：先 fetch 成 blob 再解码，和下载素材同一个思路（跨域地址直接画会污染画布、导不出）。
 * 素材地址 /files/... 在对象存储下会 302 到签名地址，所以这是一次跨域读取，要求存储放行 GET 的 CORS。
 * 不走浏览器缓存：页面上的 <img> 早就把同一地址缓存下来了，那份响应没有跨域头，fetch 复用它会被拦。
 * 失败原因分开写进错误文案并打到控制台，方便区分是跨域、状态码还是解码问题。
 * @param src 原图地址
 * @param fetcher 取数函数，测试里注入
 * @param decode 解码函数，测试里注入
 * @throws Error，文案可直接显示在节点上
 */
export async function loadSourceImage(
  src: string,
  fetcher: typeof fetch = fetch,
  decode: (blob: Blob) => Promise<ImageBitmap> = (blob) => createImageBitmap(blob),
): Promise<ImageBitmap> {
  let response: Response;
  try {
    response = await fetcher(src, { cache: "no-store" });
  } catch (error) {
    console.error("[outpaint] 读取原图被拦截（多半是对象存储没放行跨域 GET）", src, error);
    throw new Error("无法读取原图：请求被拦截，可能是对象存储没有放行跨域读取（GET）");
  }
  if (!response.ok) {
    console.error("[outpaint] 读取原图失败", src, response.status);
    throw new Error(`无法读取原图：服务器返回 ${response.status}`);
  }
  try {
    return await decode(await response.blob());
  } catch (error) {
    console.error("[outpaint] 原图解码失败", src, error);
    throw new Error("无法读取原图：图片格式无法解码");
  }
}
