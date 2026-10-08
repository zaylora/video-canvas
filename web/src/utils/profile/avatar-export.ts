/** 导出头像的边长（像素） */
export const AVATAR_EXPORT_SIZE = 512;

/** WebP 导出质量 */
const WEBP_QUALITY = 0.9;

/** 裁剪选区，单位是原图像素（react-easy-crop 的 croppedAreaPixels） */
export type CropArea = {
  /** 左上角 x */
  x: number;
  /** 左上角 y */
  y: number;
  /** 宽 */
  width: number;
  /** 高 */
  height: number;
};

/**
 * 加载图片，供 canvas 绘制
 * @param src 图片地址（object URL）
 */
const loadImage = (src: string) =>
  new Promise<HTMLImageElement>((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("图片读取失败"));
    image.src = src;
  });

/**
 * canvas 导出成 Blob
 * @param canvas 画布
 * @param type 编码类型
 * @param quality 质量（只对有损格式生效）
 */
const toBlob = (canvas: HTMLCanvasElement, type: string, quality?: number) =>
  new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, quality));

/**
 * 把选区导出成 512×512 的头像：优先 WebP（quality 0.9），浏览器不支持 WebP 编码时
 * （toBlob 返回空或退回成 PNG）改导 PNG。GIF 只取当前帧，所以动图会变成静态图。
 * @param src 原图地址
 * @param area 选区（原图像素）
 * @returns 头像图片
 */
export async function exportAvatar(src: string, area: CropArea): Promise<Blob> {
  const image = await loadImage(src);
  const canvas = document.createElement("canvas");
  canvas.width = AVATAR_EXPORT_SIZE;
  canvas.height = AVATAR_EXPORT_SIZE;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("浏览器不支持图片导出");
  context.imageSmoothingQuality = "high";
  context.drawImage(
    image,
    area.x,
    area.y,
    area.width,
    area.height,
    0,
    0,
    AVATAR_EXPORT_SIZE,
    AVATAR_EXPORT_SIZE,
  );
  const webp = await toBlob(canvas, "image/webp", WEBP_QUALITY);
  if (webp && webp.type === "image/webp") return webp;
  const png = await toBlob(canvas, "image/png");
  if (!png) throw new Error("图片导出失败");
  return png;
}
