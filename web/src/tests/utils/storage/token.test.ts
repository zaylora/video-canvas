import { beforeEach, describe, expect, test } from "bun:test";

import { getRole, getToken, removeToken, saveRole, setToken } from "@/utils/storage/token";

/** bun 测试环境没有 DOM，用内存实现顶替 localStorage */
function installLocalStorage() {
  const data = new Map<string, string>();
  (globalThis as { localStorage?: Storage }).localStorage = {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, String(value)),
    removeItem: (key: string) => void data.delete(key),
    clear: () => data.clear(),
    key: () => null,
    length: 0,
  } as Storage;
}

const farFuture = Math.floor(Date.now() / 1000) + 3600;

describe("登录态存储", () => {
  beforeEach(installLocalStorage);

  test("setToken 带角色时 getRole 能读回", () => {
    setToken("t", farFuture, "admin");
    expect(getToken()).toBe("t");
    expect(getRole()).toBe("admin");
  });

  test("没带角色（老会话）时 getRole 返回 null", () => {
    setToken("t", farFuture);
    expect(getRole()).toBeNull();
  });

  test("重新登录不带角色会清掉上一个账号的角色", () => {
    setToken("t1", farFuture, "super_admin");
    setToken("t2", farFuture);
    expect(getRole()).toBeNull();
  });

  test("saveRole 给老会话补存角色；没登录时不写", () => {
    setToken("t", farFuture);
    saveRole("super_admin");
    expect(getRole()).toBe("super_admin");
    removeToken();
    saveRole("admin");
    expect(getRole()).toBeNull();
  });

  test("removeToken 同时清掉角色", () => {
    setToken("t", farFuture, "admin");
    removeToken();
    expect(getToken()).toBeNull();
    expect(getRole()).toBeNull();
  });

  test("token 过期时角色一并失效", () => {
    setToken("t", Math.floor(Date.now() / 1000) - 10, "admin");
    expect(getToken()).toBeNull();
    expect(getRole()).toBeNull();
  });
});
