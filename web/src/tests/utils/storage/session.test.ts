import { beforeEach, describe, expect, test } from "bun:test";

import { clearSession } from "@/utils/storage/session";
import { getRole, getToken, setToken } from "@/utils/storage/token";

/** 内存版 Storage */
function memoryStorage(): Storage {
  const data = new Map<string, string>();
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, String(value)),
    removeItem: (key: string) => void data.delete(key),
    clear: () => data.clear(),
    key: (index: number) => [...data.keys()][index] ?? null,
    get length() {
      return data.size;
    },
  } as Storage;
}

describe("clearSession（退出登录要清掉的浏览器数据）", () => {
  beforeEach(() => {
    const g = globalThis as { localStorage?: Storage; sessionStorage?: Storage };
    g.localStorage = memoryStorage();
    g.sessionStorage = memoryStorage();
  });

  test("清掉令牌、过期时间与角色", () => {
    setToken("t", Math.floor(Date.now() / 1000) + 3600, "admin");
    clearSession();
    expect(getToken()).toBeNull();
    expect(getRole()).toBeNull();
    expect(localStorage.length).toBe(0);
  });

  test("不动其他会话数据", () => {
    sessionStorage.setItem("other", "keep");
    clearSession();
    expect(sessionStorage.getItem("other")).toBe("keep");
  });

  test("不清界面偏好（侧边栏、密度等不是登录数据）", () => {
    localStorage.setItem("sidebar", "false");
    clearSession();
    expect(localStorage.getItem("sidebar")).toBe("false");
  });

  test("sessionStorage 不可用时也不抛错", () => {
    (globalThis as { sessionStorage?: Storage }).sessionStorage = undefined;
    expect(() => clearSession()).not.toThrow();
  });
});
