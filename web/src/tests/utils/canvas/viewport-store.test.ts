import { describe, expect, test } from "bun:test";

import {
  clearUserViewports,
  createViewportWriter,
  loadViewport,
  sameViewport,
  saveViewport,
} from "@/utils/canvas/viewport-store";

function fakeStorage(): Storage & { map: Map<string, string> } {
  const map = new Map<string, string>();
  return {
    map,
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (key) => map.get(key) ?? null,
    key: (index) => [...map.keys()][index] ?? null,
    removeItem: (key) => void map.delete(key),
    setItem: (key, value) => void map.set(key, value),
  };
}

describe("viewport-store", () => {
  test("存进去能读回来", () => {
    const storage = fakeStorage();
    saveViewport("7", "c1", { x: 10, y: -20, zoom: 1.5 }, storage);
    expect(loadViewport("7", "c1", storage)).toEqual({ x: 10, y: -20, zoom: 1.5 });
  });

  test("按用户和画布隔离", () => {
    const storage = fakeStorage();
    saveViewport("7", "c1", { x: 1, y: 2, zoom: 1 }, storage);
    expect(loadViewport("8", "c1", storage)).toBeNull();
    expect(loadViewport("7", "c2", storage)).toBeNull();
  });

  test("值被改坏或缺字段：当作没有，不抛错", () => {
    const storage = fakeStorage();
    storage.setItem("viewport:7:c1", "not json");
    storage.setItem("viewport:7:c2", JSON.stringify({ x: 1, y: 2 }));
    storage.setItem("viewport:7:c3", JSON.stringify({ x: "1", y: 2, zoom: 1 }));
    storage.setItem("viewport:7:c4", JSON.stringify({ x: 1, y: 2, zoom: 0 }));
    storage.setItem("viewport:7:c5", JSON.stringify({ x: 1, y: Infinity, zoom: 1 }));
    for (const id of ["c1", "c2", "c3", "c4", "c5"])
      expect(loadViewport("7", id, storage)).toBeNull();
  });

  test("存储不可用（隐私模式抛错）：读写都不抛错", () => {
    const broken = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    } as unknown as Storage;
    expect(loadViewport("7", "c1", broken)).toBeNull();
    saveViewport("7", "c1", { x: 1, y: 2, zoom: 1 }, broken);
  });

  test("没有存储对象：不抛错", () => {
    expect(loadViewport("7", "c1", undefined)).toBeNull();
    saveViewport("7", "c1", { x: 1, y: 2, zoom: 1 }, undefined);
  });
});

describe("createViewportWriter", () => {
  function timers() {
    let seq = 0;
    const pending = new Map<number, { at: number; fn: () => void }>();
    let now = 0;
    return {
      setTimer: (fn: () => void, ms: number) => {
        const id = ++seq;
        pending.set(id, { at: now + ms, fn });
        return id;
      },
      clearTimer: (id: unknown) => void pending.delete(id as number),
      advance(ms: number) {
        now += ms;
        for (const [id, item] of [...pending]) {
          if (item.at <= now) {
            pending.delete(id);
            item.fn();
          }
        }
      },
    };
  }

  test("连续移动：每次移动重新计时，停下 200ms 后只写最后一次", () => {
    const writes: unknown[] = [];
    const t = timers();
    const writer = createViewportWriter((vp) => writes.push(vp), 200, t);
    writer.schedule({ x: 1, y: 0, zoom: 1 });
    t.advance(150);
    writer.schedule({ x: 2, y: 0, zoom: 1 });
    t.advance(150);
    expect(writes).toEqual([]);
    t.advance(60);
    expect(writes).toEqual([{ x: 2, y: 0, zoom: 1 }]);
  });

  test("flush：立即写出还没写的，没有待写时什么都不做", () => {
    const writes: unknown[] = [];
    const t = timers();
    const writer = createViewportWriter((vp) => writes.push(vp), 200, t);
    writer.flush();
    expect(writes).toEqual([]);
    writer.schedule({ x: 5, y: 5, zoom: 2 });
    writer.flush();
    expect(writes).toEqual([{ x: 5, y: 5, zoom: 2 }]);
    t.advance(500);
    expect(writes.length).toBe(1);
  });
});

describe("sameViewport", () => {
  test("位置和缩放都一样才算相同", () => {
    expect(sameViewport({ x: 1, y: 2, zoom: 1 }, { x: 1, y: 2, zoom: 1 })).toBe(true);
    expect(sameViewport({ x: 1, y: 2, zoom: 1 }, { x: 2, y: 2, zoom: 1 })).toBe(false);
    expect(sameViewport({ x: 1, y: 2, zoom: 1 }, { x: 1, y: 3, zoom: 1 })).toBe(false);
    expect(sameViewport({ x: 1, y: 2, zoom: 1 }, { x: 1, y: 2, zoom: 1.2 })).toBe(false);
  });

  test("浮点误差内算相同：恢复视口那一下不能被当成用户移动", () => {
    expect(sameViewport({ x: 1, y: 2, zoom: 1 }, { x: 1 + 1e-9, y: 2, zoom: 1 })).toBe(true);
  });
});

describe("clearUserViewports", () => {
  test("只清这个用户的视口，不误删前缀相同的其他用户，也不动别的键", () => {
    const storage = fakeStorage();
    saveViewport("7", "c1", { x: 1, y: 1, zoom: 1 }, storage);
    saveViewport("7", "c2", { x: 2, y: 2, zoom: 1 }, storage);
    saveViewport("70", "c1", { x: 3, y: 3, zoom: 1 }, storage);
    storage.setItem("theme", "dark");
    clearUserViewports("7", storage);
    expect(loadViewport("7", "c1", storage)).toBeNull();
    expect(loadViewport("7", "c2", storage)).toBeNull();
    expect(loadViewport("70", "c1", storage)).not.toBeNull();
    expect(storage.getItem("theme")).toBe("dark");
  });

  test("存储不可用：不抛错", () => {
    clearUserViewports("7", undefined);
    const broken = {
      get length() {
        throw new Error("denied");
      },
    } as unknown as Storage;
    clearUserViewports("7", broken);
  });
});
