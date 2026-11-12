import { describe, expect, test } from "bun:test";

import type { Reverse, Round, Script, ScriptShot } from "@/api/marketing/type";
import {
  compareState,
  formatSeconds,
  defaultSelection,
  nextSelection,
  refTimeAt,
  resolveSelection,
  scriptDuration,
  selectionKeys,
  shotIndexAt,
} from "@/utils/marketing/workbench";

const shot = (start: number, end: number, refShot: number | null = null): ScriptShot => ({
  start,
  end,
  purpose: "",
  shot: "特写",
  camera: "固定",
  scene: "画面",
  narration: "旁白",
  refShot,
});

const script = (patch: Partial<Script> = {}): Script => ({
  id: "s1",
  roundId: "r1",
  kind: "follow",
  angle: "follow",
  title: "跟拍",
  hook: "",
  shots: [shot(0, 2.5, 1), shot(2.5, 7, 2), shot(7, 15, 3)],
  music: "",
  voice: "",
  flags: [],
  status: "done",
  edited: false,
  method: null,
  takes: [],
  renderCredits: 90,
  error: "",
  revision: 1,
  ...patch,
});

const refShot = (id: number, start: number, end: number) => ({
  id,
  start,
  end,
  isOpening3s: start < 3,
  shotSize: "",
  camera: "",
  subject: "",
  action: "动作",
  scene: "",
  lighting: "",
  person: "",
  productInFrame: "",
  productRole: "none" as const,
  dialogue: "",
  onscreenText: "",
  keyColors: [],
});

const reverse: Reverse = {
  overall: { style: "", narrativeArc: "", whyViral: "", transcript: "", category: "" },
  shots: [refShot(1, 0, 3), refShot(2, 3, 9), refShot(3, 9, 12)],
  brands: [],
  claims: [],
};

const round = (patch: Partial<Round> = {}): Round => ({
  id: "r1",
  no: 1,
  status: "ready",
  stage: "",
  error: "",
  warnings: [],
  settings: {
    platform: "douyin",
    language: "zh",
    angles: [],
    autoCount: 2,
    videoModel: "seedance",
    textModel: "",
    params: {},
    description: "",
  },
  refVideo: { id: "9", url: "https://x/ref.mp4", duration: 12 },
  steps: [],
  planCount: 2,
  reverse,
  reverseRevision: 1,
  reverseEdited: false,
  reverseDirty: false,
  scripts: [
    script({ id: "a", status: "draft" }),
    script({ id: "b", kind: "create", angle: "pain", status: "done" }),
  ],
  frozen: 0,
  charged: 0,
  createdAt: "",
  ...patch,
});

describe("shotIndexAt", () => {
  const shots = [shot(0, 2.5), shot(2.5, 7), shot(7, 15)];
  test("落在镜头区间内返回该镜头", () => {
    expect(shotIndexAt(shots, 0)).toBe(0);
    expect(shotIndexAt(shots, 3)).toBe(1);
    expect(shotIndexAt(shots, 14.9)).toBe(2);
  });
  test("恰好在边界属于下一镜；末尾和超出返回最后一镜", () => {
    expect(shotIndexAt(shots, 2.5)).toBe(1);
    expect(shotIndexAt(shots, 15)).toBe(2);
    expect(shotIndexAt(shots, 99)).toBe(2);
  });
  test("没有镜头返回 -1", () => {
    expect(shotIndexAt([], 1)).toBe(-1);
  });
});

describe("refTimeAt", () => {
  test("跟拍版按对应参考镜头分段线性映射", () => {
    const s = script();
    // 成片镜头 2（2.5–7s）对应参考镜头 2（3–9s）：中点 4.75 → 6
    expect(refTimeAt(4.75, s, reverse, 12)).toBeCloseTo(6, 5);
    expect(refTimeAt(0, s, reverse, 12)).toBeCloseTo(0, 5);
  });
  test("改编版或缺映射时按时长比例", () => {
    const s = script({ kind: "create", shots: [shot(0, 5), shot(5, 15)] });
    expect(refTimeAt(7.5, s, reverse, 12)).toBeCloseTo(6, 5);
  });
  test("找不到参考镜头时退回比例；结果夹在原片时长内", () => {
    const s = script({ shots: [shot(0, 15, 99)] });
    expect(refTimeAt(15, s, reverse, 12)).toBeCloseTo(11.95, 5);
    expect(refTimeAt(-1, s, reverse, 12)).toBe(0);
  });
  test("脚本时长取最后一镜终点", () => {
    expect(scriptDuration(script())).toBe(15);
    expect(scriptDuration(script({ shots: [] }))).toBe(0);
  });
});

describe("选中项", () => {
  test("默认选第一份已出片的脚本", () => {
    expect(defaultSelection(round())).toBe("b");
  });
  test("没有已出片时有原片选原片，否则第一份脚本", () => {
    const r = round({ scripts: [script({ id: "a", status: "draft" })] });
    expect(defaultSelection(r)).toBe("ref");
    expect(defaultSelection({ ...r, refVideo: null })).toBe("a");
  });
  test("写脚本中有原片时选原片；什么都没有返回 null", () => {
    expect(defaultSelection(round({ status: "scripting", scripts: [] }))).toBe("ref");
    expect(defaultSelection(round({ status: "failed", scripts: [], refVideo: null }))).toBeNull();
  });
  test("列表顺序：原片在前，脚本按后端顺序", () => {
    expect(selectionKeys(round())).toEqual(["ref", "a", "b"]);
    expect(selectionKeys(round({ refVideo: null }))).toEqual(["a", "b"]);
  });
  test("当前选中还在就保留，不在了回落到默认", () => {
    expect(resolveSelection(round(), "a")).toBe("a");
    expect(resolveSelection(round(), "gone")).toBe("b");
    expect(resolveSelection(round({ refVideo: null }), "ref")).toBe("b");
    expect(resolveSelection(round(), null)).toBe("b");
  });
  test("上下切换到头不越界", () => {
    expect(nextSelection(round(), "ref", 1)).toBe("a");
    expect(nextSelection(round(), "b", 1)).toBe("b");
    expect(nextSelection(round(), "ref", -1)).toBe("ref");
    expect(nextSelection(round(), null, 1)).toBe("ref");
  });
});

describe("compareState", () => {
  test("有参考片且脚本已出片才能对比", () => {
    const r = round();
    expect(compareState(r, r.scripts[1]).ok).toBe(true);
  });
  test("不能对比时给出原因", () => {
    const r = round();
    expect(compareState(r, r.scripts[0])).toEqual({ ok: false, reason: "出片后才能对比" });
    expect(compareState({ ...r, refVideo: null }, r.scripts[1])).toEqual({
      ok: false,
      reason: "这一轮没有参考片",
    });
    expect(compareState(r, null)).toEqual({ ok: false, reason: "选中一份已出片的脚本后可对比" });
  });
});

describe("formatSeconds", () => {
  test("最多一位小数，整数不带 .0", () => {
    expect(formatSeconds(14.833)).toBe("14.8s");
    expect(formatSeconds(15)).toBe("15s");
    expect(formatSeconds(9.96)).toBe("10s");
    expect(formatSeconds(0)).toBe("0s");
  });
});
