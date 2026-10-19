import { describe, expect, test } from "bun:test";

import { readFlag, writeFlag, type FlagStorage } from "@/utils/director/prefs";

const memory = (initial: Record<string, string> = {}): FlagStorage => {
  const data = { ...initial };
  return {
    getItem: (key) => data[key] ?? null,
    setItem: (key, value) => void (data[key] = value),
  };
};

describe("导演台偏好", () => {
  test("没存过用默认值", () => {
    expect(readFlag("assets", true, memory())).toBe(true);
    expect(readFlag("assets", false, memory())).toBe(false);
  });

  test("写进去能读出来", () => {
    const storage = memory();
    writeFlag("assets", false, storage);
    expect(readFlag("assets", true, storage)).toBe(false);
    writeFlag("assets", true, storage);
    expect(readFlag("assets", false, storage)).toBe(true);
  });

  test("存储不可用（隐私模式、被禁用）时不抛错，读默认值", () => {
    const broken: FlagStorage = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    };
    expect(() => writeFlag("assets", false, broken)).not.toThrow();
    expect(readFlag("assets", true, broken)).toBe(true);
  });

  test("没有 localStorage 的环境也不抛错", () => {
    expect(readFlag("assets", true, undefined)).toBe(true);
    expect(() => writeFlag("assets", false, undefined)).not.toThrow();
  });
});
