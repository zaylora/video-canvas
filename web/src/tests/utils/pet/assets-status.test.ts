import { describe, expect, test } from "bun:test";

import { cornerLayout, petAssetsStatus } from "@/utils/pet/assets-status";

const base = { enabled: true, ready: false, failed: false, timedOut: false };

describe("petAssetsStatus", () => {
  test("隐藏档不加载：off，哪怕别的条件都满足", () => {
    expect(petAssetsStatus({ ...base, enabled: false, ready: true })).toBe("off");
  });

  test("刚开始加载：loading", () => {
    expect(petAssetsStatus(base)).toBe("loading");
  });

  test("素材到位：ready", () => {
    expect(petAssetsStatus({ ...base, ready: true })).toBe("ready");
  });

  test("加载失败：failed", () => {
    expect(petAssetsStatus({ ...base, failed: true })).toBe("failed");
  });

  test("等太久：按 failed 处理，让胶囊先顶上", () => {
    expect(petAssetsStatus({ ...base, timedOut: true })).toBe("failed");
  });

  test("超时后素材又到了：以 ready 为准", () => {
    expect(petAssetsStatus({ ...base, timedOut: true, ready: true })).toBe("ready");
  });
});

describe("cornerLayout", () => {
  test("加载期间：不画胶囊，但给统计条留出桌宠的位置", () => {
    expect(cornerLayout("loading")).toEqual({ showLauncher: false, reserveForPet: true });
  });

  test("桌宠就绪：不画胶囊，统计条让位", () => {
    expect(cornerLayout("ready")).toEqual({ showLauncher: false, reserveForPet: true });
  });

  test("失败：退回胶囊，不再留位", () => {
    expect(cornerLayout("failed")).toEqual({ showLauncher: true, reserveForPet: false });
  });

  test("隐藏档：胶囊在，不留位", () => {
    expect(cornerLayout("off")).toEqual({ showLauncher: true, reserveForPet: false });
  });
});
