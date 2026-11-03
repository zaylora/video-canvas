import { describe, expect, test } from "bun:test";

import { isAssistantHotkey } from "@/pages/admin/assistant/assistant-trigger";

describe("快捷键", () => {
  const key = (over: Partial<Parameters<typeof isAssistantHotkey>[0]> = {}) => ({
    key: "j",
    ctrlKey: true,
    metaKey: false,
    shiftKey: false,
    altKey: false,
    ...over,
  });

  test("Ctrl+J 和 ⌘J 都算；大写（开了 CapsLock）也算", () => {
    expect(isAssistantHotkey(key())).toBe(true);
    expect(isAssistantHotkey(key({ ctrlKey: false, metaKey: true }))).toBe(true);
    expect(isAssistantHotkey(key({ key: "J" }))).toBe(true);
  });

  test("不带修饰键、带了 Shift 或 Alt、按的是别的键都不算", () => {
    expect(isAssistantHotkey(key({ ctrlKey: false }))).toBe(false);
    expect(isAssistantHotkey(key({ shiftKey: true }))).toBe(false);
    expect(isAssistantHotkey(key({ altKey: true }))).toBe(false);
    expect(isAssistantHotkey(key({ key: "k" }))).toBe(false);
  });
});
