import { describe, expect, test } from "bun:test";

import type { FrameCanvas } from "@/utils/canvas/frame-capture";
import {
  OutpaintError,
  composeOutpaint,
  loadOutpaintImage,
  type OutpaintDeps,
} from "@/utils/canvas/outpaint-compose";
import { OUTPAINT_MAX_EDGE, frameByMult } from "@/utils/canvas/outpaint";

type FetchCall = { url: string; init: RequestInit };

/** 假 fetch：按脚本返回成功、HTTP 错误、网络 / 跨域失败，或一直不返回；记下每次调用 */
function fakeFetch(options: {
  cors?: "ok" | "fail" | "never";
  status?: number;
  /** 带 mode: "no-cors" 的探测请求能不能通 */
  reachable?: boolean;
}) {
  const calls: FetchCall[] = [];
  const fetch: OutpaintDeps["fetch"] = (url, init) => {
    calls.push({ url, init });
    if (init.mode === "no-cors") {
      return options.reachable === false
        ? Promise.reject(new TypeError("Failed to fetch"))
        : Promise.resolve({ ok: false, status: 0, blob: () => Promise.resolve(new Blob()) });
    }
    if (options.cors === "never") return new Promise(() => {});
    if (options.cors === "fail") return Promise.reject(new TypeError("Failed to fetch"));
    const status = options.status ?? 200;
    return Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      blob: () => Promise.resolve(new Blob(["x"], { type: "image/png" })),
    });
  };
  return { fetch, calls };
}

function fakeCanvas(options: { toBlob?: "ok" | "taint" | "null"; noContext?: boolean } = {}) {
  const draws: number[][] = [];
  const canvas = {
    width: 0,
    height: 0,
    getContext: () =>
      options.noContext
        ? null
        : {
            drawImage: (_i: unknown, x: number, y: number, w: number, h: number) =>
              void draws.push([x, y, w, h]),
          },
    toBlob: (cb: (blob: Blob | null) => void, type?: string) => {
      if (options.toBlob === "taint") throw new DOMException("tainted", "SecurityError");
      cb(options.toBlob === "null" ? null : new Blob(["x"], { type }));
    },
  } as unknown as FrameCanvas;
  return { canvas, draws };
}

const deps = (options: {
  fetch?: OutpaintDeps["fetch"];
  decode?: OutpaintDeps["decode"];
  canvas?: FrameCanvas;
}): OutpaintDeps => ({
  fetch: options.fetch ?? fakeFetch({}).fetch,
  decode: options.decode ?? (async () => ({ width: 1920, height: 1080 })),
  createCanvas: () => options.canvas ?? fakeCanvas().canvas,
});

describe("loadOutpaintImage：读原图", () => {
  test("跨域读取且绕过缓存（节点里的 img 先前不带跨域头加载过，缓存里没有 CORS 头）；读出像素尺寸", async () => {
    const { fetch, calls } = fakeFetch({});
    const loaded = await loadOutpaintImage(
      "/files/a.png",
      deps({ fetch, decode: async () => ({ width: 1600, height: 900 }) }),
    );
    expect(calls[0]?.url).toBe("/files/a.png");
    expect(calls[0]?.init).toMatchObject({ mode: "cors", cache: "no-store" });
    expect(loaded.size).toEqual({ width: 1600, height: 900 });
  });

  test("读取被拒但探测请求能通：是存储没开跨域读取", async () => {
    const { fetch } = fakeFetch({ cors: "fail", reachable: true });
    const error = await loadOutpaintImage("/files/a.png", deps({ fetch })).catch((e) => e);
    expect(error).toBeInstanceOf(OutpaintError);
    expect(error.code).toBe("cors");
    expect(error.message).toContain("CORS");
  });

  test("读取失败、探测也不通：地址打不开（网络、地址失效）", async () => {
    const { fetch } = fakeFetch({ cors: "fail", reachable: false });
    const error = await loadOutpaintImage("/files/a.png", deps({ fetch })).catch((e) => e);
    expect(error.code).toBe("unreadable");
  });

  test("服务端返回 404 等错误状态：读不出", async () => {
    const { fetch } = fakeFetch({ status: 404 });
    const error = await loadOutpaintImage("/files/a.png", deps({ fetch })).catch((e) => e);
    expect(error.code).toBe("unreadable");
  });

  test("取到了但解码失败（格式不支持）：读不出", async () => {
    const error = await loadOutpaintImage(
      "/files/a.png",
      deps({ decode: () => Promise.reject(new Error("bad")) }),
    ).catch((e) => e);
    expect(error.code).toBe("unreadable");
  });

  test("一直不返回：超时", async () => {
    const { fetch } = fakeFetch({ cors: "never" });
    const error = await loadOutpaintImage("/files/a.png", deps({ fetch }), {
      timeoutMs: 20,
    }).catch((e) => e);
    expect(error.code).toBe("timeout");
  });

  test("尺寸为 0（没解出来）：读不出", async () => {
    const error = await loadOutpaintImage(
      "/files/a.png",
      deps({ decode: async () => ({ width: 0, height: 0 }) }),
    ).catch((e) => e);
    expect(error.code).toBe("unreadable");
  });
});

describe("composeOutpaint：把原图拼进更大的透明画布", () => {
  const loaded = async (w = 1920, h = 1080) => {
    const { canvas, draws } = fakeCanvas();
    const d = deps({ canvas, decode: async () => ({ width: w, height: h }) });
    const image = await loadOutpaintImage("/files/a.png", d);
    return { image, canvas, draws, d };
  };

  test("画布尺寸 = 框的输出尺寸，原图画在偏移处，别处保持透明（不填色）", async () => {
    const { image, canvas, draws, d } = await loaded();
    const file = await composeOutpaint(image, frameByMult(image.size, 1.5), "扩图.png", d);
    expect([canvas.width, canvas.height]).toEqual([2880, 1620]);
    expect(draws).toEqual([[480, 270, 1920, 1080]]);
    expect(file).toBeInstanceOf(File);
    expect(file.name).toBe("扩图.png");
    expect(file.type).toBe("image/png");
  });

  test("框被平移过（原图不在中间）：原图的偏移跟着变", async () => {
    const { image, draws, d } = await loaded();
    const frame = { x0: -960, y0: 0, x1: 1920, y1: 1080 };
    await composeOutpaint(image, frame, "a.png", d);
    expect(draws).toEqual([[960, 0, 1920, 1080]]);
  });

  test("最长边超过上限：整体等比缩小，原图也按同一比例缩", async () => {
    const { image, canvas, draws, d } = await loaded();
    await composeOutpaint(image, frameByMult(image.size, 3), "a.png", d);
    expect(Math.max(canvas.width, canvas.height)).toBe(OUTPAINT_MAX_EDGE);
    const scale = OUTPAINT_MAX_EDGE / 5760;
    expect(draws[0]?.[2]).toBeCloseTo(1920 * scale);
    expect(draws[0]?.[0]).toBeCloseTo(1920 * scale);
  });

  test("画布被污染：导出抛 SecurityError，翻译成 cors，文案说明要配 CORS", async () => {
    const canvas = fakeCanvas({ toBlob: "taint" }).canvas;
    const d = deps({ canvas });
    const image = await loadOutpaintImage("/files/a.png", d);
    const error = await composeOutpaint(image, frameByMult(image.size, 1.5), "a.png", d).catch(
      (e) => e,
    );
    expect(error.code).toBe("cors");
    expect(error.message).toContain("CORS");
  });

  test("导出得到空结果：读不出画面", async () => {
    const canvas = fakeCanvas({ toBlob: "null" }).canvas;
    const d = deps({ canvas });
    const image = await loadOutpaintImage("/files/a.png", d);
    const error = await composeOutpaint(image, frameByMult(image.size, 1.5), "a.png", d).catch(
      (e) => e,
    );
    expect(error.code).toBe("unreadable");
  });

  test("浏览器拿不到 2d 画布：不支持", async () => {
    const canvas = fakeCanvas({ noContext: true }).canvas;
    const d = deps({ canvas });
    const image = await loadOutpaintImage("/files/a.png", d);
    const error = await composeOutpaint(image, frameByMult(image.size, 1.5), "a.png", d).catch(
      (e) => e,
    );
    expect(error.code).toBe("unsupported");
  });
});
