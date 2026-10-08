import { describe, expect, test } from "bun:test";

import type { MeDto } from "@/api/me/type";
import { createMeStore } from "@/store/me";

const ME: MeDto = {
  id: "42",
  username: "lin",
  nickname: "",
  email: "a@b.com",
  role: "user",
  avatarUrl: null,
  createdAt: "2025-03-12T00:00:00Z",
  emailVerifiedAt: null,
};

describe("当前用户 store", () => {
  test("fetchMe 成功后写入 me，状态为 ready", async () => {
    const store = createMeStore({ getMe: async () => ME });
    expect(store.getState().status).toBe("idle");
    await store.getState().fetchMe();
    expect(store.getState().me).toEqual(ME);
    expect(store.getState().status).toBe("ready");
  });

  test("同一时刻多处触发只发一次请求", async () => {
    let calls = 0;
    const store = createMeStore({
      getMe: async () => {
        calls += 1;
        return ME;
      },
    });
    await Promise.all([store.getState().fetchMe(), store.getState().fetchMe()]);
    expect(calls).toBe(1);
  });

  test("失败时保留旧值并标记 error", async () => {
    let fail = false;
    const store = createMeStore({
      getMe: async () => {
        if (fail) throw new Error("boom");
        return ME;
      },
    });
    await store.getState().fetchMe();
    fail = true;
    await store.getState().fetchMe();
    expect(store.getState().me).toEqual(ME);
    expect(store.getState().status).toBe("error");
  });

  test("setMe 用接口返回的新资料替换（改昵称、换头像后全站同步）", () => {
    const store = createMeStore({ getMe: async () => ME });
    store.getState().setMe({ ...ME, nickname: "林间" });
    expect(store.getState().me?.nickname).toBe("林间");
    expect(store.getState().status).toBe("ready");
  });
});
