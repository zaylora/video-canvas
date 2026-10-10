import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import {
  OUTPAINT_HANDLES,
  OUTPAINT_MAX_EDGE,
  OUTPAINT_MAX_MULT,
  OUTPAINT_RATIOS,
  buildOutpaintPrompt,
  containRect,
  detectRatio,
  formatMult,
  frameByMult,
  frameByRatio,
  frameHeight,
  frameWidth,
  longMult,
  moveFrame,
  outpaintBlocker,
  outputSize,
  resizeFrame,
  ratioText,
  type OutpaintFrame,
} from "@/utils/canvas/outpaint";

const WIDE = { width: 1920, height: 1080 };
const TALL = { width: 1080, height: 1920 };
const SQUARE_ISH = { width: 1600, height: 1200 };
const SIZES = [WIDE, TALL, SQUARE_ISH];

/** 框完整包住原图：原图左上角是原点 */
const contains = (size: { width: number; height: number }, f: OutpaintFrame) =>
  f.x0 <= 1e-6 && f.y0 <= 1e-6 && f.x1 >= size.width - 1e-6 && f.y1 >= size.height - 1e-6;
const longEdge = (f: OutpaintFrame) => Math.max(frameWidth(f), frameHeight(f));

/** 可复现的伪随机，失败时能重放 */
function rng(seed: number) {
  let s = seed;
  return () => {
    s = (s * 1664525 + 1013904223) % 4294967296;
    return s / 4294967296;
  };
}

describe("预设框", () => {
  test("1x 就是原图，没有任何扩出来的部分", () => {
    expect(frameByMult(WIDE, 1)).toEqual({ x0: 0, y0: 0, x1: 1920, y1: 1080 });
  });

  test("按倍数：长宽各放大，原图居中", () => {
    const f = frameByMult(WIDE, 1.5);
    expect(frameWidth(f)).toBeCloseTo(2880);
    expect(frameHeight(f)).toBeCloseTo(1620);
    expect(f.x0).toBeCloseTo(-480);
    expect(f.y0).toBeCloseTo(-270);
  });

  test("按比例：刚好包住原图的最小框，原图居中（16:9 原图选 1:1）", () => {
    const f = frameByRatio(WIDE, 1);
    expect(frameWidth(f)).toBeCloseTo(1920);
    expect(frameHeight(f)).toBeCloseTo(1920);
    expect(f.y0).toBeCloseTo(-420);
    expect(f.x0).toBeCloseTo(0);
  });

  test("按比例：比原图更宽的比例，是高度不变、宽度补足（16:9 原图选 21:9）", () => {
    const f = frameByRatio(WIDE, 21 / 9);
    expect(frameHeight(f)).toBeCloseTo(1080);
    expect(frameWidth(f)).toBeCloseTo(1080 * (21 / 9));
  });

  test("每个预设框都完整包住原图，也都在上限内（三种原图 × 全部比例）", () => {
    for (const size of SIZES) {
      for (const ratio of OUTPAINT_RATIOS) {
        const f = frameByRatio(size, ratio.ratio);
        expect(contains(size, f)).toBe(true);
        expect(longEdge(f)).toBeLessThanOrEqual(
          OUTPAINT_MAX_MULT * Math.max(size.width, size.height) + 1e-6,
        );
      }
    }
  });
});

describe("识别当前是哪个比例", () => {
  test("等于原图比例识别为原比例（优先于预设）", () => {
    expect(detectRatio(WIDE, frameByMult(WIDE, 1.5))).toBe("orig");
    expect(detectRatio(SQUARE_ISH, frameByMult(SQUARE_ISH, 2))).toBe("orig");
  });

  test("预设比例按比例识别", () => {
    expect(detectRatio(WIDE, frameByRatio(WIDE, 1))).toBe("1:1");
    expect(detectRatio(WIDE, frameByRatio(WIDE, 9 / 16))).toBe("9:16");
  });

  test("拖出自定义比例后什么都不是", () => {
    const f = resizeFrame(WIDE, "r", frameByMult(WIDE, 1), 100, 0).frame;
    expect(detectRatio(WIDE, f)).toBeNull();
  });

  test("倍数按最长边算，显示成 1.37x 这样", () => {
    expect(longMult(WIDE, frameByMult(WIDE, 1.5))).toBeCloseTo(1.5);
    expect(formatMult(1.5)).toBe("1.5x");
    expect(formatMult(1)).toBe("1x");
    expect(formatMult(1.3712)).toBe("1.37x");
  });
});

describe("平移框", () => {
  test("大小不变，不会把原图露在框外", () => {
    const start = frameByMult(WIDE, 1.5);
    const moved = moveFrame(WIDE, start, -9999, 9999);
    expect(frameWidth(moved)).toBeCloseTo(frameWidth(start));
    expect(frameHeight(moved)).toBeCloseTo(frameHeight(start));
    expect(contains(WIDE, moved)).toBe(true);
    // 往左拖到头：原图贴在框的右下角
    expect(moved.x1).toBeCloseTo(WIDE.width);
    expect(moved.y0).toBeCloseTo(0);
  });

  test("框和原图一样大时没有可动的余地", () => {
    const one = frameByMult(WIDE, 1);
    expect(moveFrame(WIDE, one, 500, 500)).toEqual(one);
  });

  test("在范围内的拖动照实平移", () => {
    const start = frameByMult(WIDE, 2);
    const moved = moveFrame(WIDE, start, 100, -50);
    expect(moved.x0).toBeCloseTo(start.x0 + 100);
    expect(moved.y0).toBeCloseTo(start.y0 - 50);
  });
});

describe("拖手柄", () => {
  test("边手柄：只动一条边，不会缩进原图里", () => {
    const start = frameByMult(WIDE, 1.5);
    const { frame } = resizeFrame(WIDE, "l", start, 99999, 0);
    expect(frame.x0).toBe(0);
    expect(frame.x1).toBe(start.x1);
    expect(frame.y0).toBe(start.y0);
  });

  test("边手柄：到 3 倍上限停住，并告知超限", () => {
    const start = frameByMult(WIDE, 2);
    const { frame, limited } = resizeFrame(WIDE, "r", start, 99999, 0);
    expect(frameWidth(frame)).toBeCloseTo(OUTPAINT_MAX_MULT * WIDE.width);
    expect(limited).toBe(true);
  });

  test("没碰到上限时不报超限", () => {
    const { limited } = resizeFrame(WIDE, "r", frameByMult(WIDE, 1.25), 10, 0);
    expect(limited).toBe(false);
  });

  test("角手柄：对角固定、比例不变", () => {
    const start = frameByRatio(WIDE, 1);
    const { frame } = resizeFrame(WIDE, "br", start, 300, 300);
    expect(frame.x0).toBeCloseTo(start.x0);
    expect(frame.y0).toBeCloseTo(start.y0);
    expect(frameWidth(frame) / frameHeight(frame)).toBeCloseTo(1);
    expect(frameWidth(frame)).toBeGreaterThan(frameWidth(start));
  });

  test("角手柄往里拖：缩到刚好包住原图为止", () => {
    const start = frameByMult(WIDE, 2);
    const { frame } = resizeFrame(WIDE, "br", start, -99999, -99999);
    expect(contains(WIDE, frame)).toBe(true);
    expect(frameWidth(frame)).toBeGreaterThanOrEqual(WIDE.width - 1e-6);
  });

  test("随机拖动（含平移后再拖）：始终包住原图、不超 3 倍；角手柄始终等比", () => {
    const random = rng(7);
    for (const size of SIZES) {
      const starts = [
        frameByMult(size, 1),
        frameByMult(size, 1.5),
        frameByMult(size, 3),
        ...OUTPAINT_RATIOS.map((r) => frameByRatio(size, r.ratio)),
      ];
      for (const start of starts) {
        for (const handle of OUTPAINT_HANDLES) {
          for (let i = 0; i < 40; i++) {
            const moved = moveFrame(size, start, (random() - 0.5) * 8000, (random() - 0.5) * 8000);
            const { frame } = resizeFrame(
              size,
              handle,
              moved,
              (random() - 0.5) * 8000,
              (random() - 0.5) * 8000,
            );
            expect(contains(size, frame)).toBe(true);
            expect(longEdge(frame)).toBeLessThanOrEqual(
              OUTPAINT_MAX_MULT * Math.max(size.width, size.height) + 1e-6,
            );
            if (handle.length === 2) {
              expect(frameWidth(frame) / frameHeight(frame)).toBeCloseTo(
                frameWidth(moved) / frameHeight(moved),
                6,
              );
            }
          }
        }
      }
    }
  });
});

describe("输出尺寸", () => {
  test("上限内按原图像素 1:1 输出", () => {
    const out = outputSize(frameByMult(WIDE, 1.5));
    expect(out).toEqual({ width: 2880, height: 1620, scale: 1 });
  });

  test("最长边超过上限时整体等比缩小，比例不变", () => {
    const out = outputSize(frameByMult(WIDE, 3));
    expect(Math.max(out.width, out.height)).toBe(OUTPAINT_MAX_EDGE);
    expect(out.scale).toBeCloseTo(OUTPAINT_MAX_EDGE / 5760);
    expect(out.width / out.height).toBeCloseTo(16 / 9, 2);
  });
});

describe("提示词与文案", () => {
  test("用户没写：只有固定指令，指令里带目标比例", () => {
    const text = buildOutpaintPrompt("", "16:9");
    expect(text).toContain("透明区域");
    expect(text).toContain("16:9");
    expect(text.endsWith("\n")).toBe(false);
  });

  test("用户写了：接在固定指令后面", () => {
    const text = buildOutpaintPrompt("  向两边延伸成海边  ", "1:1");
    expect(text).toContain("向两边延伸成海边");
    expect(text.indexOf("透明区域")).toBeLessThan(text.indexOf("向两边延伸成海边"));
    expect(text).not.toContain("  向两边");
  });

  test("比例文字：预设比例用预设名，其余写成 x.xx:1", () => {
    expect(ratioText(WIDE, frameByRatio(WIDE, 1))).toBe("1:1");
    expect(ratioText(WIDE, resizeFrame(WIDE, "r", frameByMult(WIDE, 1), 100, 0).frame)).toMatch(
      /^\d+\.\d{2}:1$/,
    );
  });
});

describe("图片在节点里的位置", () => {
  test("等比放进盒子（contain），居中；返回显示缩放", () => {
    const r = containRect({ width: 1000, height: 1000 }, { width: 576, height: 324 });
    expect(r.width).toBeCloseTo(324);
    expect(r.height).toBeCloseTo(324);
    expect(r.x).toBeCloseTo((576 - 324) / 2);
    expect(r.y).toBe(0);
    expect(r.scale).toBeCloseTo(0.324);
  });

  test("同比例时占满盒子", () => {
    const r = containRect({ width: 1920, height: 1080 }, { width: 576, height: 324 });
    expect(r.x).toBeCloseTo(0);
    expect(r.width).toBeCloseTo(576);
  });
});

describe("当前模型能不能扩图", () => {
  const caps = (patch: Partial<Capabilities>): Capabilities => ({
    refs: {
      image: { on: true, max: 4, max_mb: 10 },
      video: { on: false, max: 0, max_mb: 0 },
      audio: { on: false, max: 0, max_mb: 0 },
    },
    prompt: { max_length: 1000 },
    ops: ["t2i", "i2i"],
    ...patch,
  });

  test("支持图生图且收参考图：可以", () => {
    expect(outpaintBlocker(caps({}))).toBeNull();
  });

  test("只有文生图：不行，并说明原因", () => {
    expect(outpaintBlocker(caps({ ops: ["t2i"] }))).toContain("图生图");
  });

  test("不收参考图：不行", () => {
    const c = caps({});
    c.refs.image = { on: false, max: 0, max_mb: 0 };
    expect(outpaintBlocker(c)).toContain("参考图");
  });
});
