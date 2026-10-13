import { describe, expect, test } from "bun:test";

import {
  SILENT_PEAK,
  clockParts,
  computePeaks,
  positionToTime,
  timeToPosition,
} from "@/utils/canvas/audio-peaks";

describe("computePeaks：把整段采样均分成 N 根，取每段峰值", () => {
  test("每根取该段绝对值最大的采样，再按最大值归一化到 0~1", () => {
    const samples = new Float32Array([0.125, -0.25, 0.5, 0.25, -1, 0.5, 0, 0]);
    expect(computePeaks(samples, 4)).toEqual([0.25, 0.5, 1, 0]);
  });

  test("全静音返回全 0，不除以 0", () => {
    expect(computePeaks(new Float32Array(100), 5)).toEqual([0, 0, 0, 0, 0]);
  });

  test("采样比条数还少时，条数照旧，多出来的条取 0", () => {
    const peaks = computePeaks(new Float32Array([1, 0.5]), 4);
    expect(peaks).toHaveLength(4);
    expect(Math.max(...peaks)).toBe(1);
  });

  test("没有采样或条数无效时返回空数组", () => {
    expect(computePeaks(new Float32Array(0), 4)).toEqual([]);
    expect(computePeaks(new Float32Array(4), 0)).toEqual([]);
  });

  test("低于阈值的算静音，渲染时画成圆点", () => {
    expect(SILENT_PEAK).toBeGreaterThan(0);
    expect(SILENT_PEAK).toBeLessThan(0.1);
  });
});

describe("clockParts：时间拆成「前面的 0」和「有效位」两段", () => {
  test("1 秒 → 00:00:0 / 1", () => {
    expect(clockParts(1)).toEqual({ dim: "00:00:0", bright: "1" });
  });

  test("65 秒 → 00:0 / 1:05", () => {
    expect(clockParts(65)).toEqual({ dim: "00:0", bright: "1:05" });
  });

  test("1 小时 2 分 3 秒 → 没有暗的部分", () => {
    expect(clockParts(3723)).toEqual({ dim: "0", bright: "1:02:03" });
  });

  test("0 秒：只有最后一位是亮的", () => {
    expect(clockParts(0)).toEqual({ dim: "00:00:0", bright: "0" });
  });

  test("无效值按 0 处理，小数向下取整", () => {
    expect(clockParts(Number.NaN)).toEqual(clockParts(0));
    expect(clockParts(-3)).toEqual(clockParts(0));
    expect(clockParts(1.9)).toEqual(clockParts(1));
  });
});

describe("位置与时间互转", () => {
  test("位置 0~1 对应 0~总时长，夹在两端", () => {
    expect(positionToTime(0.5, 24)).toBe(12);
    expect(positionToTime(-1, 24)).toBe(0);
    expect(positionToTime(2, 24)).toBe(24);
  });

  test("时间转位置，总时长为 0 或无效时为 0", () => {
    expect(timeToPosition(6, 24)).toBe(0.25);
    expect(timeToPosition(30, 24)).toBe(1);
    expect(timeToPosition(5, 0)).toBe(0);
    expect(timeToPosition(5, Number.NaN)).toBe(0);
  });
});
