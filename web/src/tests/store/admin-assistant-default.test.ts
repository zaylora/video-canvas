import { describe, expect, test } from "bun:test";

import { adminPersistRules, useAdminStore, type AdminStore } from "@/store/admin";

describe("后台小镜默认关闭", () => {
  test("初始状态是收起", () => {
    expect(useAdminStore.getInitialState().assistantOpen).toBe(false);
  });

  test("展开状态不写进本地存储，下次进后台仍是收起", () => {
    expect(adminPersistRules.partialize()).toEqual({});
  });

  test("旧版本存下的「已展开」读回时被丢掉，不会让抽屉自动打开", () => {
    const current = { assistantOpen: false } as unknown as AdminStore;
    const merged = adminPersistRules.merge({ assistantOpen: true }, current);
    expect(merged.assistantOpen).toBe(false);
  });
});
