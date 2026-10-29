import { describe, expect, test } from "bun:test";

import { getPageNumbers } from "@/utils/pagination";

describe("页码按钮", () => {
  test("5 页以内全列出", () => {
    expect(getPageNumbers(1, 3)).toEqual([1, 2, 3]);
  });

  test("页多时首尾保留，中间省略", () => {
    expect(getPageNumbers(1, 6)).toEqual([1, 2, 3, 4, "...", 6]);
    expect(getPageNumbers(6, 6)).toEqual([1, "...", 3, 4, 5, 6]);
    expect(getPageNumbers(5, 10)).toEqual([1, "...", 4, 5, 6, "...", 10]);
  });
});
