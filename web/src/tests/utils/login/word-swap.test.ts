import { describe, expect, test } from "bun:test";

import { carryLabel, wordIndexAt } from "@/utils/login/word-swap";

describe("登录页标题里轮换的词", () => {
  test("每过一个间隔换下一个，到头回到第一个", () => {
    expect(wordIndexAt(0, 3, 2800)).toBe(0);
    expect(wordIndexAt(2799, 3, 2800)).toBe(0);
    expect(wordIndexAt(2800, 3, 2800)).toBe(1);
    expect(wordIndexAt(5600, 3, 2800)).toBe(2);
    expect(wordIndexAt(8400, 3, 2800)).toBe(0);
  });

  test("只有一个词或间隔无效时一直是第一个", () => {
    expect(wordIndexAt(99999, 1, 2800)).toBe(0);
    expect(wordIndexAt(99999, 3, 0)).toBe(0);
  });
});

describe("卡片顶部带过来的想法", () => {
  test("有想法时提示登录后继续，前后空白去掉", () => {
    expect(carryLabel("  雨夜街头，女主回眸  ")).toBe("登录后继续：雨夜街头，女主回眸");
  });

  test("没写想法时不显示", () => {
    expect(carryLabel("")).toBeNull();
    expect(carryLabel("   ")).toBeNull();
  });
});
