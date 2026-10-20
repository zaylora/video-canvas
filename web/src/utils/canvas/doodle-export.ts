import type { PixelSize } from "./doodle";
import type { CompositeCanvas } from "./outpaint-compose";

const createCanvas = (): CompositeCanvas =>
  document.createElement("canvas") as unknown as CompositeCanvas;

/**
 * 把标注层叠在原图上导出 PNG：画布取涂鸦的逻辑尺寸（原图像素，长边超上限时已等比缩小），
 * 原图和标注层都铺满整张画布。标注层的坐标、粗细、字号本来就是逻辑像素，所以和屏幕上看到的比例一致。
 * @param source 原图（ImageBitmap 等）
 * @param layer 标注层（Fabric 的 toCanvasElement 结果，尺寸等于逻辑尺寸）
 * @param size 输出尺寸，doodleCanvasSize 的结果
 * @param makeCanvas 造画布的函数，测试里注入假的
 * @throws 拿不到绘图上下文或导出失败时抛 Error，文案可直接显示给用户
 */
export async function renderDoodle(
  source: CanvasImageSource,
  layer: CanvasImageSource,
  size: PixelSize,
  makeCanvas: () => CompositeCanvas = createCanvas,
): Promise<Blob> {
  const canvas = makeCanvas();
  canvas.width = size.width;
  canvas.height = size.height;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("涂鸦图导出失败：浏览器不支持画布");
  // 不能先取出 context.drawImage 再调：原生方法脱离 context 会抛 "Illegal invocation"
  const draw = (...args: unknown[]) => (context.drawImage as (...args: unknown[]) => void)(...args);
  draw(source, 0, 0, size.width, size.height);
  draw(layer, 0, 0, size.width, size.height);
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("涂鸦图导出失败"))),
      "image/png",
    );
  });
}
