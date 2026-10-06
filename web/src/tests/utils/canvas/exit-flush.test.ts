import { describe, expect, test } from "bun:test";

import { flushAllExit, registerExitFlush } from "@/utils/canvas/exit-flush";

describe("exit-flush", () => {
  test("没有注册任何画布：视为全部已同步", async () => {
    expect(await flushAllExit()).toBe(true);
  });

  test("全部返回 true 才算同步完", async () => {
    const off1 = registerExitFlush(async () => true);
    const off2 = registerExitFlush(async () => false);
    expect(await flushAllExit()).toBe(false);
    off2();
    expect(await flushAllExit()).toBe(true);
    off1();
  });

  test("某个 flush 抛错：算没同步完，不影响其他", async () => {
    let called = false;
    const off1 = registerExitFlush(async () => {
      throw new Error("boom");
    });
    const off2 = registerExitFlush(async () => {
      called = true;
      return true;
    });
    expect(await flushAllExit()).toBe(false);
    expect(called).toBe(true);
    off1();
    off2();
  });

  test("注销后不再被调用", async () => {
    let calls = 0;
    const off = registerExitFlush(async () => {
      calls += 1;
      return true;
    });
    off();
    await flushAllExit();
    expect(calls).toBe(0);
  });
});
