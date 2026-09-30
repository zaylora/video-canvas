import { describe, expect, test } from "bun:test";

import { outputText } from "@/utils/tasks/status";
import { makeTask, succeeded, textSucceeded } from "./fixtures";

describe("outputText：文本产出提取", () => {
  test("取第一份带正文的产出，原样保留换行与首尾空白", () => {
    expect(outputText(textSucceeded("  你好\n世界 "))).toBe("  你好\n世界 ");
  });

  test("跳过没有 text 的产出", () => {
    const view = textSucceeded("x", {
      outputs: [{ media_type: "text", text: " " }, { media_type: "text", text: "正文" }],
    });
    expect(outputText(view)).toBe("正文");
  });

  test("没有文本产出（null、空数组、媒体产出）返回 undefined", () => {
    expect(outputText(makeTask())).toBeUndefined();
    expect(outputText(textSucceeded("x", { outputs: [] }))).toBeUndefined();
    expect(outputText(succeeded())).toBeUndefined();
  });
});
