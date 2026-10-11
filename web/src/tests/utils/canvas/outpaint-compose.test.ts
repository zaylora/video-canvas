import { describe, expect, test } from "bun:test";

import {
  COMPOSE_MAX_EDGE,
  loadSourceImage,
  planCompose,
  renderComposite,
  type CompositeCanvas,
} from "@/utils/canvas/outpaint-compose";
import { frameByMult, frameByRatio } from "@/utils/canvas/outpaint-frame";

const origin = { w: 576, h: 324 };
const natural = { w: 1920, h: 1080 };

describe("planCompose 合成方案", () => {
  test("按原图真实像素换算输出尺寸，原图贴在框内对应位置", () => {
    const plan = planCompose({ natural, origin, frame: frameByMult(origin, 1.5) });
    // 画布坐标 → 像素：1920 / 576
    expect(plan.width).toBe(2880);
    expect(plan.height).toBe(1620);
    expect(plan.imageX).toBe(480);
    expect(plan.imageY).toBe(270);
    expect(plan.imageW).toBe(1920);
    expect(plan.imageH).toBe(1080);
    expect(plan.scaled).toBe(false);
  });

  test("不扩（1 倍）时合成图就是原图", () => {
    const plan = planCompose({ natural, origin, frame: frameByMult(origin, 1) });
    expect([plan.width, plan.height]).toEqual([1920, 1080]);
    expect([plan.imageX, plan.imageY]).toEqual([0, 0]);
  });

  test("最长边超过上限时整体等比压缩", () => {
    const plan = planCompose({ natural, origin, frame: frameByMult(origin, 3) });
    // 3 倍是 5760 × 3240，压到最长边 4096
    expect(plan.width).toBe(COMPOSE_MAX_EDGE);
    expect(plan.height).toBe(2304);
    expect(plan.scaled).toBe(true);
    // 原图也按同一比例缩小：1920 × 4096 / 5760
    expect(plan.imageW).toBe(1365);
    expect(plan.imageH).toBe(768);
    expect(plan.imageX).toBe(1365);
    expect(plan.imageY).toBe(768);
  });

  test("原图贴出画布的误差会被收回，不会画到边外", () => {
    const plan = planCompose({
      natural: { w: 1001, h: 563 },
      origin,
      frame: frameByRatio(origin, 1),
    });
    expect(plan.imageX + plan.imageW).toBeLessThanOrEqual(plan.width);
    expect(plan.imageY + plan.imageH).toBeLessThanOrEqual(plan.height);
  });

  test("竖图按比例预设扩成方图", () => {
    const portrait = { w: 252, h: 448 };
    const plan = planCompose({
      natural: { w: 1080, h: 1920 },
      origin: portrait,
      frame: frameByRatio(portrait, 1),
    });
    expect(plan.width).toBe(plan.height);
    expect(plan.height).toBe(1920);
    expect(plan.imageX).toBeGreaterThan(0);
    expect(plan.imageY).toBe(0);
  });
});

/** 记录调用的假画布 */
function fakeCanvas() {
  const calls: unknown[][] = [];
  const canvas: CompositeCanvas & { width: number; height: number } = {
    width: 0,
    height: 0,
    getContext: () => ({ drawImage: (...args: unknown[]) => calls.push(args) }),
    toBlob: (cb, type) => cb(new Blob(["png"], { type: type ?? "" })),
  };
  return { canvas, calls };
}

describe("renderComposite 导出透明 PNG", () => {
  test("按方案设画布大小、贴原图并导出 PNG", async () => {
    const { canvas, calls } = fakeCanvas();
    const plan = planCompose({ natural, origin, frame: frameByMult(origin, 1.5) });
    const source = { width: 1920, height: 1080 } as unknown as CanvasImageSource;
    const blob = await renderComposite(source, plan, () => canvas);
    expect([canvas.width, canvas.height]).toEqual([2880, 1620]);
    expect(calls).toEqual([[source, 480, 270, 1920, 1080]]);
    expect(blob.type).toBe("image/png");
  });

  test("导出失败时抛出可读的错误", async () => {
    const { canvas } = fakeCanvas();
    canvas.toBlob = (cb) => cb(null);
    const plan = planCompose({ natural, origin, frame: frameByMult(origin, 1.5) });
    await expect(renderComposite({} as CanvasImageSource, plan, () => canvas)).rejects.toThrow(
      "合成图导出失败",
    );
  });
});

describe("loadSourceImage 读原图", () => {
  const bitmap = { width: 10, height: 10 } as ImageBitmap;

  test("绕过浏览器缓存取图（之前 <img> 缓存的无跨域头响应会让 fetch 被拦）", async () => {
    const seen: RequestInit[] = [];
    const fetcher = (async (_src: string, init?: RequestInit) => {
      seen.push(init ?? {});
      return new Response(new Blob(["x"]), { status: 200 });
    }) as unknown as typeof fetch;
    const image = await loadSourceImage("/files/a.png", fetcher, async () => bitmap);
    expect(image).toBe(bitmap);
    expect(seen[0].cache).toBe("no-store");
  });

  test("请求被拦截（多半是对象存储没放行跨域读取）时说清原因", async () => {
    const fetcher = (async () => {
      throw new TypeError("Failed to fetch");
    }) as unknown as typeof fetch;
    await expect(loadSourceImage("/files/a.png", fetcher)).rejects.toThrow(/跨域/);
  });

  test("服务器返回错误码时带上状态码", async () => {
    const fetcher = (async () => new Response("", { status: 403 })) as unknown as typeof fetch;
    await expect(loadSourceImage("/files/a.png", fetcher)).rejects.toThrow(/403/);
  });

  test("图片解码失败时单独说明", async () => {
    const fetcher = (async () =>
      new Response(new Blob(["x"]), { status: 200 })) as unknown as typeof fetch;
    await expect(
      loadSourceImage("/files/a.png", fetcher, async () => {
        throw new Error("bad image");
      }),
    ).rejects.toThrow(/解码/);
  });
});
