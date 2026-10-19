import { describe, expect, test } from "bun:test";

import { readNumber, writeNumber } from "@/utils/director/prefs";

const memory = () => {
  const data = new Map<string, string>();
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
  };
};

describe("数值偏好", () => {
  test("写了再读得回来，带上 director: 前缀", () => {
    const storage = memory();
    writeNumber("assets-width", 312, storage);
    expect(storage.getItem("director:assets-width")).toBe("312");
    expect(readNumber("assets-width", 272, storage)).toBe(312);
  });

  test("小数也能存（分栏比例）", () => {
    const storage = memory();
    writeNumber("split", 0.4, storage);
    expect(readNumber("split", 0.5, storage)).toBe(0.4);
  });

  test("没存过、存的不是数字时给默认值", () => {
    const storage = memory();
    expect(readNumber("x", 7, storage)).toBe(7);
    storage.setItem("director:x", "abc");
    expect(readNumber("x", 7, storage)).toBe(7);
    storage.setItem("director:x", "");
    expect(readNumber("x", 7, storage)).toBe(7);
  });

  test("存储不可用或抛错时不报错", () => {
    const broken = {
      getItem: () => {
        throw new Error("禁用");
      },
      setItem: () => {
        throw new Error("禁用");
      },
    };
    expect(readNumber("x", 5, broken)).toBe(5);
    expect(() => writeNumber("x", 1, broken)).not.toThrow();
    expect(readNumber("x", 5, undefined)).toBe(5);
  });
});
