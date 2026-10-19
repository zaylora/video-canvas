import { describe, expect, test } from "bun:test";

import type { CharacterKey } from "@/types";
import { moveKey, removeKey, setKeyEase, upsertKey } from "@/utils/director/keys";

const key = (t: number, x = 0): CharacterKey => ({ t, x, z: 0, yaw: 0, ease: "smooth" });

describe("记录关键帧", () => {
  test("按时间插入并保持升序", () => {
    const keys = upsertKey(upsertKey([key(2)], key(0)), key(1));
    expect(keys.map((k) => k.t)).toEqual([0, 1, 2]);
  });

  test("同一时刻已有关键帧则覆盖", () => {
    const keys = upsertKey([key(1, 0)], key(1, 5));
    expect(keys).toHaveLength(1);
    expect(keys[0].x).toBe(5);
  });

  test("时间精确到 0.01 秒，近似时刻视为同一帧", () => {
    const keys = upsertKey([key(1)], key(1.004, 3));
    expect(keys).toHaveLength(1);
    expect(keys[0].t).toBe(1);
  });

  test("不改动传入的数组", () => {
    const original = [key(0)];
    upsertKey(original, key(1));
    expect(original).toHaveLength(1);
  });
});

describe("删除、移动、改插值", () => {
  test("删除指定时刻", () => {
    expect(removeKey([key(0), key(1)], 1).map((k) => k.t)).toEqual([0]);
  });

  test("移动到新时刻，落点已有帧则被替换", () => {
    const moved = moveKey([key(0, 1), key(1, 2), key(2, 3)], 0, 2);
    expect(moved.map((k) => [k.t, k.x])).toEqual([
      [1, 2],
      [2, 1],
    ]);
  });

  test("移动时间不能为负", () => {
    expect(moveKey([key(1)], 1, -3)[0].t).toBe(0);
  });

  test("切换插值方式", () => {
    expect(setKeyEase([key(1)], 1, "linear")[0].ease).toBe("linear");
  });
});
