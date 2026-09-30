import { describe, expect, test } from "bun:test";

import {
  IMPORT_DRAFT_PREFIX,
  readStashedDrafts,
  stashDrafts,
  updateStashedDrafts,
} from "@/utils/admin/import-draft";

/** 内存版 Storage */
const memoryStorage = () => {
  const map = new Map<string, string>();
  return {
    map,
    getItem: (key: string) => map.get(key) ?? null,
    setItem: (key: string, value: string) => void map.set(key, value),
    removeItem: (key: string) => void map.delete(key),
  };
};

const brokenStorage = {
  getItem: () => {
    throw new Error("denied");
  },
  setItem: () => {
    throw new Error("denied");
  },
  removeItem: () => {
    throw new Error("denied");
  },
};

const payload = { channelKey: "newapi-main", bodies: [{ key: "a" }, { key: "b" }] };

describe("导入草稿暂存", () => {
  test("存进去拿到带时间戳的键，读出来内容一致；键不含草稿内容", () => {
    const storage = memoryStorage();
    const id = stashDrafts(payload, storage, 1700000000000);
    expect(id).toBe("1700000000000");
    expect(readStashedDrafts(id, storage)).toEqual(payload);
    expect(storage.map.has(IMPORT_DRAFT_PREFIX + id)).toBe(true);
  });

  test("键缺失、内容损坏、没有草稿都返回 null（不预填）", () => {
    const storage = memoryStorage();
    expect(readStashedDrafts(null, storage)).toBeNull();
    expect(readStashedDrafts("nope", storage)).toBeNull();
    storage.setItem(IMPORT_DRAFT_PREFIX + "bad", "{oops");
    expect(readStashedDrafts("bad", storage)).toBeNull();
    storage.setItem(IMPORT_DRAFT_PREFIX + "empty", JSON.stringify({ bodies: [] }));
    expect(readStashedDrafts("empty", storage)).toBeNull();
  });

  test("存储不可用或抛错时读写都不崩：存返回 null，读返回 null", () => {
    expect(stashDrafts(payload, null)).toBeNull();
    expect(stashDrafts(payload, brokenStorage)).toBeNull();
    expect(readStashedDrafts("1", brokenStorage)).toBeNull();
    expect(() => updateStashedDrafts("1", payload, brokenStorage)).not.toThrow();
    expect(() => updateStashedDrafts("1", payload, null)).not.toThrow();
  });

  test("处理掉一个后写回剩余；剩余为空时删除暂存", () => {
    const storage = memoryStorage();
    const id = stashDrafts(payload, storage, 1)!;
    updateStashedDrafts(id, { ...payload, bodies: [{ key: "b" }] }, storage);
    expect(readStashedDrafts(id, storage)?.bodies).toEqual([{ key: "b" }]);
    updateStashedDrafts(id, { ...payload, bodies: [] }, storage);
    expect(storage.map.size).toBe(0);
  });
});
