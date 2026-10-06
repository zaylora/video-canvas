import { describe, expect, test } from "bun:test";

import type { CanvasGraphDto } from "@/api/canvas/type";
import { createDraftStore, type DraftBackend } from "@/utils/canvas/draft-store";

const graph = {
  nodes: [],
  edges: [],
  viewport: { x: 0, y: 0, zoom: 1 },
} as unknown as CanvasGraphDto;

/** 内存版后端：不用真 IndexedDB 也能测存取逻辑 */
function memoryBackend(): DraftBackend & { data: Map<string, unknown> } {
  const data = new Map<string, unknown>();
  return {
    data,
    get: async (key) => data.get(key),
    put: async (key, value) => void data.set(key, value),
    delete: async (key) => void data.delete(key),
    keys: async () => [...data.keys()],
  };
}

describe("draft-store", () => {
  test("存进去能读回来，带基准版本和时间", async () => {
    const store = createDraftStore(memoryBackend(), () => 1000);
    expect(await store.save("7", "c1", { graph, baseVersion: 3, cloudDirty: true })).toBe(true);
    const draft = await store.load("7", "c1");
    expect(draft).toEqual({
      schemaVersion: 1,
      graph,
      baseVersion: 3,
      cloudDirty: true,
      savedAt: 1000,
    });
  });

  test("按 userId 隔离：换账号读不到上一个人的草稿", async () => {
    const store = createDraftStore(memoryBackend());
    await store.save("7", "c1", { graph, baseVersion: 3, cloudDirty: true });
    expect(await store.load("8", "c1")).toBeNull();
  });

  test("删除后读不到", async () => {
    const store = createDraftStore(memoryBackend());
    await store.save("7", "c1", { graph, baseVersion: 3, cloudDirty: true });
    await store.remove("7", "c1");
    expect(await store.load("7", "c1")).toBeNull();
  });

  test("读到损坏或不认识的版本：当作没有草稿，不抛错", async () => {
    const backend = memoryBackend();
    backend.data.set("7:c1", { schemaVersion: 99, graph, baseVersion: 3 });
    backend.data.set("7:c2", "garbage");
    backend.data.set("7:c3", {
      schemaVersion: 1,
      graph: null,
      baseVersion: 3,
      cloudDirty: true,
      savedAt: 1,
    });
    backend.data.set("7:c4", {
      schemaVersion: 1,
      graph,
      baseVersion: "3",
      cloudDirty: true,
      savedAt: 1,
    });
    const store = createDraftStore(backend);
    expect(await store.load("7", "c1")).toBeNull();
    expect(await store.load("7", "c2")).toBeNull();
    expect(await store.load("7", "c3")).toBeNull();
    expect(await store.load("7", "c4")).toBeNull();
  });

  test("后端写入失败：save 返回 false，不抛错", async () => {
    const backend = memoryBackend();
    backend.put = async () => {
      throw new Error("QuotaExceededError");
    };
    const store = createDraftStore(backend);
    expect(await store.save("7", "c1", { graph, baseVersion: 3, cloudDirty: true })).toBe(false);
  });

  test("后端读取失败：load 返回 null，不抛错", async () => {
    const backend = memoryBackend();
    backend.get = async () => {
      throw new Error("blocked");
    };
    const store = createDraftStore(backend);
    expect(await store.load("7", "c1")).toBeNull();
  });

  test("删除失败也不抛错", async () => {
    const backend = memoryBackend();
    backend.delete = async () => {
      throw new Error("blocked");
    };
    const store = createDraftStore(backend);
    await store.remove("7", "c1");
  });

  test("clearUser 只清这个用户的全部草稿", async () => {
    const store = createDraftStore(memoryBackend());
    await store.save("7", "c1", { graph, baseVersion: 1, cloudDirty: true });
    await store.save("7", "c2", { graph, baseVersion: 1, cloudDirty: true });
    await store.save("70", "c1", { graph, baseVersion: 1, cloudDirty: true });
    await store.clearUser("7");
    expect(await store.load("7", "c1")).toBeNull();
    expect(await store.load("7", "c2")).toBeNull();
    // "70" 以 "7" 开头，但不是同一个用户，不能被前缀误删
    expect(await store.load("70", "c1")).not.toBeNull();
  });

  test("purgeExpired 只清已同步且超过保留期的草稿，未同步的永远不清", async () => {
    let now = 0;
    const store = createDraftStore(memoryBackend(), () => now);
    const day = 24 * 60 * 60 * 1000;
    await store.save("7", "old-synced", { graph, baseVersion: 1, cloudDirty: false });
    await store.save("7", "old-dirty", { graph, baseVersion: 1, cloudDirty: true });
    now = 20 * day;
    await store.save("7", "fresh-synced", { graph, baseVersion: 1, cloudDirty: false });
    now = 31 * day;
    await store.purgeExpired("7", 30 * day);
    expect(await store.load("7", "old-synced")).toBeNull();
    expect(await store.load("7", "old-dirty")).not.toBeNull();
    expect(await store.load("7", "fresh-synced")).not.toBeNull();
  });
});
