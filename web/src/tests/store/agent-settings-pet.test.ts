import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import {
  DEFAULT_PET_SETTINGS,
  migrateAgentSettings,
  useAgentSettings,
} from "@/store/agent-settings";

/** bun 的测试环境没有 localStorage，放一个最小的内存实现，免得 persist 反复告警 */
const memory = new Map<string, string>();
beforeAll(() => {
  (globalThis as { localStorage?: unknown }).localStorage = {
    getItem: (key: string) => memory.get(key) ?? null,
    setItem: (key: string, value: string) => void memory.set(key, value),
    removeItem: (key: string) => void memory.delete(key),
  };
});
afterAll(() => {
  delete (globalThis as { localStorage?: unknown }).localStorage;
});

describe("桌宠设置默认值", () => {
  test("活泼档、默认右下角、内置皮肤小镜", () => {
    expect(DEFAULT_PET_SETTINGS).toEqual({ mode: "lively", anchor: null, skin: "xiaojing" });
    expect(useAgentSettings.getState().pet).toEqual(DEFAULT_PET_SETTINGS);
  });

  test("patchPet 只改给出的字段", () => {
    useAgentSettings.getState().patchPet({ mode: "quiet" });
    expect(useAgentSettings.getState().pet).toEqual({ ...DEFAULT_PET_SETTINGS, mode: "quiet" });
    useAgentSettings.getState().patchPet({ anchor: { corner: "tl", dx: 20, dy: 30 } });
    expect(useAgentSettings.getState().pet.mode).toBe("quiet");
    expect(useAgentSettings.getState().pet.anchor).toEqual({ corner: "tl", dx: 20, dy: 30 });
    useAgentSettings.getState().patchPet(DEFAULT_PET_SETTINGS);
  });

  test("不同实例的默认值互不污染", () => {
    expect(DEFAULT_PET_SETTINGS.anchor).toBeNull();
    const a = migrateAgentSettings({}, 0) as { pet: { mode: string } };
    a.pet.mode = "hidden";
    expect(DEFAULT_PET_SETTINGS.mode).toBe("lively");
  });
});

describe("persist 迁移 migrateAgentSettings", () => {
  test("旧数据（版本 0）没有 pet：补默认值，其余字段原样保留", () => {
    const old = { modelKey: "gpt", showThinking: true, offset: { dx: 3, dy: 4 }, docked: true };
    const out = migrateAgentSettings(old, 0) as Record<string, unknown>;
    expect(out.pet).toEqual(DEFAULT_PET_SETTINGS);
    expect(out.modelKey).toBe("gpt");
    expect(out.showThinking).toBe(true);
    expect(out.offset).toEqual({ dx: 3, dy: 4 });
    expect(out.docked).toBe(true);
  });

  test("已有合法 pet：保留用户的选择", () => {
    const pet = { mode: "quiet", anchor: { corner: "bl", dx: 40, dy: 50 }, skin: "xiaojing" };
    const out = migrateAgentSettings({ pet }, 1) as { pet: unknown };
    expect(out.pet).toEqual(pet);
  });

  test("pet 缺字段或字段非法：逐项回退到默认值", () => {
    const out = migrateAgentSettings(
      { pet: { mode: "weird", anchor: { corner: "zz", dx: 1, dy: 1 } } },
      1,
    ) as { pet: unknown };
    expect(out.pet).toEqual(DEFAULT_PET_SETTINGS);
    const half = migrateAgentSettings({ pet: { mode: "hidden" } }, 1) as { pet: unknown };
    expect(half.pet).toEqual({ ...DEFAULT_PET_SETTINGS, mode: "hidden" });
  });

  test("存的不是对象（损坏）：给出只有 pet 的默认对象，让 persist 与默认状态合并", () => {
    expect(migrateAgentSettings(null, 0)).toEqual({ pet: DEFAULT_PET_SETTINGS });
    expect(migrateAgentSettings("x", 0)).toEqual({ pet: DEFAULT_PET_SETTINGS });
  });
});
