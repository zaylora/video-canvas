import { describe, expect, test } from "bun:test";

import { renderDoodle } from "@/utils/canvas/doodle-export";
import type { CompositeCanvas } from "@/utils/canvas/outpaint-compose";

/**
 * 记录调用的假画布。drawImage 和浏览器的原生方法一样认 this：
 * 脱离 context 单独调用会抛 "Illegal invocation"，不能用箭头函数糊弄过去
 */
function fakeCanvas() {
  const calls: unknown[][] = [];
  const context = {
    drawImage(this: unknown, ...args: unknown[]) {
      if (this !== context) throw new TypeError("Illegal invocation");
      calls.push(args);
    },
  };
  const canvas: CompositeCanvas = {
    width: 0,
    height: 0,
    getContext: () => context,
    toBlob: (cb, type) => cb(new Blob(["png"], { type: type ?? "" })),
  };
  return { canvas, calls };
}

const base = { width: 1280, height: 720 } as unknown as CanvasImageSource;
const layer = { width: 1280, height: 720 } as unknown as CanvasImageSource;

describe("renderDoodle 把标注叠在原图上导出", () => {
  test("画布是逻辑尺寸，先画原图再画标注层，都铺满", async () => {
    const { canvas, calls } = fakeCanvas();
    const blob = await renderDoodle(base, layer, { width: 1280, height: 720 }, () => canvas);
    expect([canvas.width, canvas.height]).toEqual([1280, 720]);
    expect(calls).toEqual([
      [base, 0, 0, 1280, 720],
      [layer, 0, 0, 1280, 720],
    ]);
    expect(blob.type).toBe("image/png");
  });

  test("原图比逻辑尺寸大（长边被压缩）时，原图缩到逻辑尺寸再叠", async () => {
    const { canvas, calls } = fakeCanvas();
    const big = { width: 8192, height: 4096 } as unknown as CanvasImageSource;
    await renderDoodle(big, layer, { width: 4096, height: 2048 }, () => canvas);
    expect(calls[0]).toEqual([big, 0, 0, 4096, 2048]);
    expect(calls[1]).toEqual([layer, 0, 0, 4096, 2048]);
  });

  test("拿不到绘图上下文时抛出可读的错误", async () => {
    const { canvas } = fakeCanvas();
    canvas.getContext = () => null;
    await expect(
      renderDoodle(base, layer, { width: 10, height: 10 }, () => canvas),
    ).rejects.toThrow("涂鸦图导出失败：浏览器不支持画布");
  });

  test("导出失败时抛出可读的错误", async () => {
    const { canvas } = fakeCanvas();
    canvas.toBlob = (cb) => cb(null);
    await expect(
      renderDoodle(base, layer, { width: 10, height: 10 }, () => canvas),
    ).rejects.toThrow("涂鸦图导出失败");
  });
});
