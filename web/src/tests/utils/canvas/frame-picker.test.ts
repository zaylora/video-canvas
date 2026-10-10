import { describe, expect, test } from "bun:test";

import {
  FRAME_STEP,
  MAX_STAGED_FRAMES,
  canStageFrame,
  ratioOfTime,
  stepFrameTime,
  thumbCount,
  thumbTimes,
  timeFromRatio,
} from "@/utils/canvas/frame-picker";

describe("胶片缩略图", () => {
  test("格数按轨道宽度算，宽就多、窄就少，有上下限", () => {
    expect(thumbCount(576)).toBe(9);
    expect(thumbCount(100)).toBe(4);
    expect(thumbCount(5000)).toBe(14);
  });

  test("每格取该段正中间的时刻，第一格不是 0、最后一格不是尾", () => {
    const times = thumbTimes(8, 4);
    expect(times).toEqual([1, 3, 5, 7]);
  });

  test("时长为 0 或格数为 0 时没有缩略图", () => {
    expect(thumbTimes(0, 4)).toEqual([]);
    expect(thumbTimes(8, 0)).toEqual([]);
  });
});

describe("播放头位置与时刻互换", () => {
  test("按比例取时刻：0 是首帧，1 是尾帧（略早于时长）", () => {
    expect(timeFromRatio(0, 8)).toBe(0);
    expect(timeFromRatio(0.5, 8)).toBe(4);
    expect(timeFromRatio(1, 8)).toBeLessThan(8);
    expect(timeFromRatio(1, 8)).toBeGreaterThan(7.9);
  });

  test("比例越界时收回范围内", () => {
    expect(timeFromRatio(-1, 8)).toBe(0);
    expect(timeFromRatio(5, 8)).toBe(timeFromRatio(1, 8));
  });

  test("时刻转比例：落在 0 到 1，时长未知时为 0", () => {
    expect(ratioOfTime(2, 8)).toBe(0.25);
    expect(ratioOfTime(20, 8)).toBe(1);
    expect(ratioOfTime(1, 0)).toBe(0);
  });
});

describe("键盘微调", () => {
  test("方向键走一帧，按住 Shift 走一秒，不越界", () => {
    expect(stepFrameTime(4, 1, false, 8)).toBeCloseTo(4 + FRAME_STEP);
    expect(stepFrameTime(4, -1, false, 8)).toBeCloseTo(4 - FRAME_STEP);
    expect(stepFrameTime(4, 1, true, 8)).toBe(5);
    expect(stepFrameTime(0.2, -1, true, 8)).toBe(0);
    expect(stepFrameTime(7.9, 1, true, 8)).toBe(timeFromRatio(1, 8));
  });
});

describe("暂存帧：能不能再收一张", () => {
  test("没有重复也没超限就收", () => {
    expect(canStageFrame([{ time: 1 }], 3)).toEqual({ ok: true });
  });

  test("同一时刻（一帧之内）已经收过：拒绝，说明是重复", () => {
    expect(canStageFrame([{ time: 3 }], 3.01)).toEqual({ ok: false, reason: "duplicate" });
  });

  test("相邻的不同帧不算重复", () => {
    expect(canStageFrame([{ time: 3 }], 3 + FRAME_STEP)).toEqual({ ok: true });
  });

  test("到上限不能再收", () => {
    const staged = Array.from({ length: MAX_STAGED_FRAMES }, (_, i) => ({ time: i }));
    expect(canStageFrame(staged, 99)).toEqual({ ok: false, reason: "limit" });
  });
});
