import { describe, expect, test } from "bun:test";

import { BUBBLE_MAX_CHARS, clampBubbleText } from "@/utils/pet/bubble-text";

describe("clampBubbleText", () => {
  test("不超长原样返回", () => {
    expect(clampBubbleText("好啦，回复写完了")).toBe("好啦，回复写完了");
  });

  test("超出 40 个字用省略号截断", () => {
    expect(clampBubbleText("字".repeat(60))).toBe("字".repeat(BUBBLE_MAX_CHARS) + "…");
  });

  test("按码点截断，不把表情切成两半", () => {
    expect(clampBubbleText("😀".repeat(50))).toBe("😀".repeat(BUBBLE_MAX_CHARS) + "…");
  });

  test("换行压成空格", () => {
    expect(clampBubbleText("卡住了：\n原因")).toBe("卡住了： 原因");
  });
});
