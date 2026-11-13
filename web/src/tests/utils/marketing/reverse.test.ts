import { describe, expect, test } from "bun:test";

import type { Reverse, ReverseShot, Round, Script } from "@/api/marketing/type";
import {
  isReverseDirty,
  patchReverseShot,
  refollowState,
  reverseEditable,
  reverseSummary,
  validateAction,
} from "@/utils/marketing/reverse";

const shot = (id: number, patch: Partial<ReverseShot> = {}): ReverseShot => ({
  id,
  start: (id - 1) * 2,
  end: id * 2,
  isOpening3s: id <= 2,
  shotSize: "特写",
  camera: "手持",
  subject: "",
  action: `动作 ${id} 左手划过干纹`,
  scene: "",
  lighting: "",
  person: "",
  productInFrame: "",
  productRole: "none",
  dialogue: `台词 ${id}`,
  onscreenText: "",
  keyColors: [],
  ...patch,
});

const reverse = (shots: ReverseShot[]): Reverse => ({
  overall: { style: "", narrativeArc: "", whyViral: "前 2 秒", transcript: "", category: "" },
  shots,
  brands: ["某品牌"],
  claims: ["销量第一", "第二支半价"],
});

const follow = (status: Script["status"]): Script =>
  ({ id: "f", kind: "follow", status }) as Script;

const round = (patch: Partial<Round> = {}): Round =>
  ({
    id: "r1",
    status: "ready",
    reverse: reverse([shot(1), shot(2)]),
    reverseDirty: false,
    reverseEdited: false,
    reverseRevision: 1,
    scripts: [follow("draft")],
    ...patch,
  }) as Round;

describe("拆解是否有改动（决定「按拆解重写跟拍版」）", () => {
  test("服务端说 dirty 就是 dirty；本地草稿与服务端不同也算", () => {
    expect(isReverseDirty(round(), null)).toBe(false);
    expect(isReverseDirty(round({ reverseDirty: true }), null)).toBe(true);
    const draft = reverse([shot(1, { action: "改了动作" }), shot(2)]);
    expect(isReverseDirty(round(), draft)).toBe(true);
  });

  test("只比较用户能改的四个字段：动作 / 台词 / 商品出现方式 / 关键颜色", () => {
    const base = round();
    expect(isReverseDirty(base, reverse([shot(1, { subject: "别的" }), shot(2)]))).toBe(false);
    expect(isReverseDirty(base, reverse([shot(1, { productRole: "hero_real" }), shot(2)]))).toBe(
      true,
    );
    expect(isReverseDirty(base, reverse([shot(1, { keyColors: ["白"] }), shot(2)]))).toBe(true);
    expect(isReverseDirty(base, reverse([shot(1, { dialogue: "改台词" }), shot(2)]))).toBe(true);
  });

  test("没有拆解不算 dirty", () => {
    expect(isReverseDirty(round({ reverse: null }), null)).toBe(false);
  });
});

describe("按拆解重写跟拍版", () => {
  const cost = 3;
  test("没改动：禁用写原因；改了且余额够：可点", () => {
    expect(refollowState(round(), null, 100, cost)).toEqual({
      disabled: true,
      reason: "拆解没有改动",
    });
    expect(refollowState(round({ reverseDirty: true }), null, 100, cost)).toEqual({
      disabled: false,
      reason: "只重跑跟拍改写",
    });
  });

  test("跟拍版正在重写或出片：禁用", () => {
    for (const status of ["rewriting", "rendering"] as const) {
      expect(
        refollowState(round({ reverseDirty: true, scripts: [follow(status)] }), null, 100, cost),
      ).toEqual({ disabled: true, reason: "跟拍版正在生成或出片" });
    }
  });

  test("余额不足：禁用", () => {
    expect(refollowState(round({ reverseDirty: true }), null, 2, cost)).toEqual({
      disabled: true,
      reason: "余额不足",
    });
  });

  test("拆解在编辑时（跟拍版出片中）整张表只读", () => {
    expect(reverseEditable(round({ scripts: [follow("rendering")] }))).toBe(false);
    expect(reverseEditable(round({ scripts: [follow("rewriting")] }))).toBe(false);
    expect(reverseEditable(round())).toBe(true);
  });
});

describe("拆解编辑", () => {
  test("改某个镜头的字段，不改原对象", () => {
    const original = reverse([shot(1), shot(2)]);
    const next = patchReverseShot(original, 1, { dialogue: "新台词" });
    expect(next.shots[1].dialogue).toBe("新台词");
    expect(original.shots[1].dialogue).toBe("台词 2");
    expect(next.shots[0]).toBe(original.shots[0]);
  });

  test("动作写得太短不保存", () => {
    expect(validateAction("")).not.toBeNull();
    expect(validateAction("手")).not.toBeNull();
    expect(validateAction("右手拇指按压软管")).toBeNull();
  });

  test("折叠摘要：镜头数、时长、为什么火、品牌 / 背书数量", () => {
    expect(reverseSummary(reverse([shot(1), shot(2)]))).toEqual({
      shotCount: 2,
      seconds: 4,
      whyViral: "前 2 秒",
      brandCount: 1,
      claimCount: 2,
    });
  });
});
