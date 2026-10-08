import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { saveCarry, takeCarry } from "@/utils/home/carry";

/** 测试环境没有浏览器，用内存实现顶一个 sessionStorage */
const memoryStorage = () => {
  const data = new Map<string, string>();
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
    removeItem: (key: string) => void data.delete(key),
  };
};

const original = Object.getOwnPropertyDescriptor(globalThis, "sessionStorage");

beforeEach(() => {
  Object.defineProperty(globalThis, "sessionStorage", {
    value: memoryStorage(),
    configurable: true,
  });
});

afterEach(() => {
  if (original) Object.defineProperty(globalThis, "sessionStorage", original);
  else delete (globalThis as { sessionStorage?: unknown }).sessionStorage;
});

describe("做同款带入的提示词", () => {
  test("存进去的话只能取到一次", () => {
    saveCarry("夜晚的城市俯瞰，车流像发光的河");
    expect(takeCarry()).toBe("夜晚的城市俯瞰，车流像发光的河");
    expect(takeCarry()).toBe("");
  });

  test("首尾空白会被去掉，纯空白不记", () => {
    saveCarry("  一句话  ");
    expect(takeCarry()).toBe("一句话");
    saveCarry("   ");
    expect(takeCarry()).toBe("");
  });

  test("sessionStorage 不可用时静默降级，不抛错", () => {
    Object.defineProperty(globalThis, "sessionStorage", {
      get() {
        throw new Error("denied");
      },
      configurable: true,
    });
    expect(() => saveCarry("一句话")).not.toThrow();
    expect(takeCarry()).toBe("");
  });
});
