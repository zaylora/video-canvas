import { describe, expect, test } from "bun:test";

import { costValue, fallbackUnit, withCostValue } from "@/utils/admin/price-cost";

describe("costValue：规格 / 默认价自己的积分成本", () => {
  test("没填成本或没开启时为 undefined", () => {
    expect(costValue("per_call", undefined)).toBeUndefined();
    expect(costValue("per_call", { on: false, unit: 3 })).toBeUndefined();
  });

  test("按次读 unit，按秒读 per_second，开启但没填按 0", () => {
    expect(costValue("per_call", { on: true, unit: 3 })).toBe(3);
    expect(costValue("per_second", { on: true, per_second: 2 })).toBe(2);
    expect(costValue("per_call", { on: true })).toBe(0);
  });
});

describe("withCostValue", () => {
  test("按计费方式写到对应字段并开启", () => {
    expect(withCostValue("per_call", undefined, 5)).toEqual({ on: true, unit: 5 });
    expect(withCostValue("per_second", { on: true, unit: 1 }, 2)).toEqual({
      on: true,
      unit: 1,
      per_second: 2,
    });
  });
});

describe("fallbackUnit：按规格定价时隐藏的兜底价", () => {
  const tier = (unit: number, on = true) => ({ on, when: { op: "t2v" }, unit });

  test("取启用规格里最低的价", () => {
    expect(fallbackUnit([tier(8), tier(3), tier(5)])).toBe(3);
  });

  test("没有启用的规格时取全部里最低的", () => {
    expect(fallbackUnit([tier(8, false), tier(6, false)])).toBe(6);
  });

  test("没有规格为 undefined", () => {
    expect(fallbackUnit([])).toBeUndefined();
  });
});
