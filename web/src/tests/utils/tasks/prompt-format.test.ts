import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import { PROMPT_FORMATS, promptFormatLabel, withPromptFormat } from "@/utils/tasks/capabilities";

const base: Capabilities = {
  refs: {
    image: { on: true, max: 9, max_mb: 10 },
    audio: { on: false, max: 0, max_mb: 0 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 2000 },
};

describe("提示词格式", () => {
  test("可选项依次是通用、Seedance、MiniMax H3", () => {
    expect(PROMPT_FORMATS).toEqual([
      { value: "", label: "通用" },
      { value: "seedance", label: "Seedance" },
      { value: "h3", label: "MiniMax H3" },
    ]);
  });

  test("选择格式后 capabilities 带 prompt_format，其余能力原样保留", () => {
    const next = withPromptFormat(base, "seedance");
    expect(next.prompt_format).toBe("seedance");
    expect(next.refs).toEqual(base.refs);
  });

  test("选通用时不写这个键，旧值也被清掉", () => {
    const next = withPromptFormat({ ...base, prompt_format: "h3" }, "");
    expect("prompt_format" in next).toBe(false);
  });

  test("旧数据缺省显示通用；已有值显示对应名称", () => {
    expect(promptFormatLabel(undefined)).toBe("通用");
    expect(promptFormatLabel("")).toBe("通用");
    expect(promptFormatLabel("h3")).toBe("MiniMax H3");
  });
});
