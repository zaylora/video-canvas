import { describe, expect, test } from "bun:test";

import { canPeek, peekPlacement, peekSize, type PeekRect } from "@/utils/canvas/ref-peek";

const rect = (left: number, top: number, width = 48, height = 48): PeekRect => ({
  left,
  top,
  width,
  height,
  right: left + width,
  bottom: top + height,
});

const viewport = { width: 1000, height: 800 };
const size = { width: 200, height: 240 };

describe("canPeek 判断这个引用要不要弹放大预览", () => {
  test("有地址的图片、视频弹", () => {
    expect(canPeek({ kind: "image", src: "/a.png" })).toBe(true);
    expect(canPeek({ kind: "video", src: "/a.mp4" })).toBe(true);
  });

  test("以素材类型为准：视频节点上挂的是图片也按图片弹", () => {
    expect(canPeek({ kind: "video", mediaType: "image", src: "/a.png" })).toBe(true);
  });

  test("音频不弹", () => {
    expect(canPeek({ kind: "audio", src: "/a.mp3" })).toBe(false);
    expect(canPeek({ kind: "image", mediaType: "audio", src: "/a.mp3" })).toBe(false);
  });

  test("文本来源不弹，哪怕带着正文", () => {
    expect(canPeek({ kind: "script", src: null })).toBe(false);
    expect(canPeek({ kind: "script", src: "/x" })).toBe(false);
  });

  test("没有地址（还没生成、上传中）不弹", () => {
    expect(canPeek({ kind: "image" })).toBe(false);
    expect(canPeek({ kind: "image", src: null })).toBe(false);
    expect(canPeek({ kind: "video", src: "" })).toBe(false);
  });

  test("已断开的引用不弹", () => {
    expect(canPeek({ kind: "image", src: "/a.png", gone: true })).toBe(false);
  });
});

describe("peekSize 按比例缩进最大框", () => {
  test("小图保持原尺寸，不放大", () => {
    expect(peekSize({ width: 100, height: 120 })).toEqual({ width: 100, height: 120 });
  });

  test("宽图被宽度卡住", () => {
    expect(peekSize({ width: 1024, height: 512 })).toEqual({ width: 256, height: 128 });
  });

  test("竖图被高度卡住", () => {
    expect(peekSize({ width: 576, height: 1152 })).toEqual({ width: 144, height: 288 });
  });

  test("可以指定最大框", () => {
    expect(peekSize({ width: 400, height: 400 }, { width: 100, height: 200 })).toEqual({
      width: 100,
      height: 100,
    });
  });

  test("还不知道原始尺寸时按最大框估", () => {
    expect(peekSize(null)).toEqual({ width: 256, height: 288 });
    expect(peekSize({ width: 0, height: 0 })).toEqual({ width: 256, height: 288 });
  });
});

describe("peekPlacement 决定预览摆在目标的哪一侧", () => {
  test("默认在目标正上方 14px，水平居中", () => {
    const place = peekPlacement({ anchor: rect(400, 500), size, viewport });
    expect(place.side).toBe("above");
    expect(place.top).toBe(500 - 14 - 240);
    expect(place.left).toBe(400 + 24 - 100);
  });

  test("可以改间距", () => {
    const place = peekPlacement({ anchor: rect(400, 500), size, viewport, gap: 20 });
    expect(place.top).toBe(500 - 20 - 240);
  });

  test("上方刚好留够边距时仍在上方", () => {
    const place = peekPlacement({ anchor: rect(400, 240 + 14 + 8), size, viewport });
    expect(place.side).toBe("above");
    expect(place.top).toBe(8);
  });

  test("上方放不下就翻到目标下方", () => {
    const place = peekPlacement({ anchor: rect(400, 100), size, viewport });
    expect(place.side).toBe("below");
    expect(place.top).toBe(100 + 48 + 14);
  });

  test("贴近左边缘时钳在左边距上", () => {
    const place = peekPlacement({ anchor: rect(0, 500), size, viewport });
    expect(place.left).toBe(8);
  });

  test("贴近右边缘时钳在右边距上", () => {
    const place = peekPlacement({ anchor: rect(990, 500, 8), size, viewport });
    expect(place.left).toBe(1000 - 200 - 8);
  });

  test("视口比预览还窄时优先保证左边不出屏幕", () => {
    const place = peekPlacement({
      anchor: rect(50, 500),
      size,
      viewport: { width: 150, height: 800 },
    });
    expect(place.left).toBe(8);
  });
});
