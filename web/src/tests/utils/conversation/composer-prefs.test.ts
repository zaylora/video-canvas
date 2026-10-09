import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { DEFAULT_PREFS, loadPrefs, savePrefs } from "@/utils/conversation/composer-prefs";

const memoryStorage = () => {
  const data = new Map<string, string>();
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
    removeItem: (key: string) => void data.delete(key),
    data,
  };
};

const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");

afterEach(() => {
  if (original) Object.defineProperty(globalThis, "localStorage", original);
  else delete (globalThis as { localStorage?: unknown }).localStorage;
});

describe("输入卡片偏好（localStorage）", () => {
  let storage: ReturnType<typeof memoryStorage>;
  beforeEach(() => {
    storage = memoryStorage();
    Object.defineProperty(globalThis, "localStorage", { value: storage, configurable: true });
  });

  test("没存过：默认图片模式、没有记住的模型和参数", () => {
    expect(loadPrefs()).toEqual(DEFAULT_PREFS);
    expect(DEFAULT_PREFS.mode).toBe("image");
  });

  test("存了再读：模式、按模式记的模型、按模型记的参数都原样回来", () => {
    savePrefs({
      mode: "video",
      modelByMode: { image: "a", video: "b" },
      paramsByModel: { b: { duration: 10, aspect_ratio: "16:9" } },
    });
    expect(loadPrefs()).toEqual({
      mode: "video",
      modelByMode: { image: "a", video: "b" },
      paramsByModel: { b: { duration: 10, aspect_ratio: "16:9" } },
    });
  });

  test("内容被改坏：非法模式回到默认，不认识的字段丢掉，不抛错", () => {
    storage.setItem(
      "vc.composer.prefs",
      JSON.stringify({
        mode: "agent",
        modelByMode: { image: 1, video: "ok", audio: null, evil: "x" },
        paramsByModel: { m: "oops", n: { a: 1 } },
      }),
    );
    expect(loadPrefs()).toEqual({
      mode: "image",
      modelByMode: { video: "ok" },
      paramsByModel: { n: { a: 1 } },
    });
    storage.setItem("vc.composer.prefs", "{坏的");
    expect(loadPrefs()).toEqual(DEFAULT_PREFS);
  });

  test("存储不可用（隐私模式抛错）：读取回默认，保存静默跳过", () => {
    Object.defineProperty(globalThis, "localStorage", {
      get() {
        throw new Error("denied");
      },
      configurable: true,
    });
    expect(loadPrefs()).toEqual(DEFAULT_PREFS);
    expect(() => savePrefs(DEFAULT_PREFS)).not.toThrow();
  });

  test("按模型记的参数最多保留 30 个，超出时丢最早的", () => {
    const paramsByModel: Record<string, Record<string, unknown>> = {};
    for (let i = 0; i < 35; i += 1) paramsByModel[`m${i}`] = { i };
    savePrefs({ ...DEFAULT_PREFS, paramsByModel });
    const loaded = loadPrefs().paramsByModel;
    expect(Object.keys(loaded)).toHaveLength(30);
    expect(loaded.m34).toEqual({ i: 34 });
    expect(loaded.m0).toBeUndefined();
  });
});
