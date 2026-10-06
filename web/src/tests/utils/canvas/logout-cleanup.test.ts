import { describe, expect, test } from "bun:test";

import { cleanupBeforeLogout } from "@/utils/canvas/logout-cleanup";

function setup({ flush = true as boolean | "hang" } = {}) {
  const calls: string[] = [];
  const deps = {
    userId: "7" as string | null,
    flushAll: () => (flush === "hang" ? new Promise<boolean>(() => {}) : Promise.resolve(flush)),
    clearDrafts: async (id: string) => void calls.push(`drafts:${id}`),
    clearViewports: (id: string) => void calls.push(`viewports:${id}`),
    wait: (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, Math.min(ms, 5))),
  };
  return { deps, calls };
}

describe("cleanupBeforeLogout", () => {
  test("云端都同步完：清草稿和视口", async () => {
    const { deps, calls } = setup();
    const result = await cleanupBeforeLogout(deps);
    expect(calls.sort()).toEqual(["drafts:7", "viewports:7"]);
    expect(result).toEqual({ draftsCleared: true });
  });

  test("云端没同步完（失败或冲突）：保留草稿，下次登录同一账号还能恢复；视口照清", async () => {
    const { deps, calls } = setup({ flush: false });
    const result = await cleanupBeforeLogout(deps);
    expect(calls).toEqual(["viewports:7"]);
    expect(result).toEqual({ draftsCleared: false });
  });

  test("同步超时卡住：不能卡住退出登录，按没同步完处理", async () => {
    const { deps, calls } = setup({ flush: "hang" });
    const result = await cleanupBeforeLogout({ ...deps, timeoutMs: 1000 });
    expect(calls).toEqual(["viewports:7"]);
    expect(result.draftsCleared).toBe(false);
  });

  test("认不出当前用户：什么都不清", async () => {
    const { deps, calls } = setup();
    const result = await cleanupBeforeLogout({ ...deps, userId: null });
    expect(calls).toEqual([]);
    expect(result).toEqual({ draftsCleared: false });
  });

  test("清理本身抛错也不能让退出登录失败", async () => {
    const { deps } = setup();
    deps.clearDrafts = async () => {
      throw new Error("blocked");
    };
    await cleanupBeforeLogout(deps);
  });
});
