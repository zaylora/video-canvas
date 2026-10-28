import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { readPrefs, useStudioPrefsStore } from "@/store/drama-studio";

/** bun 的测试环境没有 localStorage，放一个最小的内存实现 */
const memory = new Map<string, string>();
const fakeStorage = {
  getItem: (key: string) => memory.get(key) ?? null,
  setItem: (key: string, value: string) => void memory.set(key, value),
};

beforeEach(() => {
  memory.clear();
  (globalThis as { localStorage?: unknown }).localStorage = fakeStorage;
  useStudioPrefsStore.setState({ byDrama: {} });
});
afterEach(() => {
  delete (globalThis as { localStorage?: unknown }).localStorage;
});

describe("片段工作区偏好（按短剧存在本机）", () => {
  test("没存过：空偏好", () => {
    expect(readPrefs("d1")).toEqual({ videoModel: "", imageModel: "", overrides: {} });
  });

  test("patch 写进内存与本机，下次读得到；不同短剧互不影响", () => {
    useStudioPrefsStore.getState().patch("d1", { videoModel: "veo" });
    useStudioPrefsStore.getState().patch("d1", { overrides: { resolution: "1080P" } });
    expect(useStudioPrefsStore.getState().get("d1")).toEqual({
      videoModel: "veo",
      imageModel: "",
      overrides: { resolution: "1080P" },
    });
    expect(readPrefs("d1").videoModel).toBe("veo");
    expect(readPrefs("d2").videoModel).toBe("");
  });

  test("存的内容坏了或被改成别的形状：当没存过，不抛", () => {
    memory.set("drama-studio:d1", "{不是 json");
    expect(readPrefs("d1").videoModel).toBe("");
    memory.set("drama-studio:d1", JSON.stringify({ videoModel: 3, overrides: [1] }));
    expect(readPrefs("d1")).toEqual({ videoModel: "", imageModel: "", overrides: {} });
  });

  test("localStorage 不可用（隐私模式）：读写都不抛", () => {
    delete (globalThis as { localStorage?: unknown }).localStorage;
    expect(readPrefs("d1").videoModel).toBe("");
    expect(() => useStudioPrefsStore.getState().patch("d1", { videoModel: "x" })).not.toThrow();
    expect(useStudioPrefsStore.getState().get("d1").videoModel).toBe("x");
  });
});
