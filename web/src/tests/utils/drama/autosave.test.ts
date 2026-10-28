import { describe, expect, test } from "bun:test";

import { Autosaver, type AutosaveStatus } from "@/utils/drama/autosave";

const CONFLICT = new Error("conflict");
const isConflict = (e: unknown) => e === CONFLICT;

/** 轮询等待条件成立，超时则失败 */
async function until(check: () => boolean, ms = 1000) {
  const start = Date.now();
  while (!check()) {
    if (Date.now() - start > ms) throw new Error("等待超时");
    await new Promise((r) => setTimeout(r, 2));
  }
}

describe("带 revision 的自动保存", () => {
  test("停手后保存，并用返回的新版本号做下一次的乐观锁", async () => {
    const calls: [string, number][] = [];
    const saver = new Autosaver({
      revision: 3,
      delay: 5,
      isConflict,
      save: async (value, revision) => {
        calls.push([value, revision]);
        return revision + 1;
      },
    });
    saver.reset("a", 3);
    saver.update("ab");
    await until(() => saver.getStatus() === "saved");
    saver.update("abc");
    await until(() => calls.length === 2 && saver.getStatus() === "saved");
    expect(calls).toEqual([
      ["ab", 3],
      ["abc", 4],
    ]);
    expect(saver.getRevision()).toBe(5);
  });

  test("连续输入只保存最后一版", async () => {
    const saves: string[] = [];
    const saver = new Autosaver({
      revision: 1,
      delay: 10,
      isConflict,
      save: async (value, revision) => {
        saves.push(value);
        return revision + 1;
      },
    });
    saver.reset("", 1);
    for (const v of ["a", "ab", "abc"]) saver.update(v);
    await until(() => saver.getStatus() === "saved");
    expect(saves).toEqual(["abc"]);
  });

  test("改回已保存的内容等于没改，不发请求", async () => {
    let n = 0;
    const saver = new Autosaver({
      revision: 1,
      delay: 5,
      isConflict,
      save: async (_v, r) => {
        n += 1;
        return r + 1;
      },
    });
    saver.reset("a", 1);
    saver.update("ab");
    saver.update("a");
    expect(saver.getStatus()).toBe("saved");
    await new Promise((r) => setTimeout(r, 30));
    expect(n).toBe(0);
  });

  test("保存中又有新改动：接着再存一次，不并发", async () => {
    let active = 0;
    let maxActive = 0;
    const saves: string[] = [];
    const saver = new Autosaver({
      revision: 1,
      delay: 1,
      isConflict,
      save: async (value, revision) => {
        active += 1;
        maxActive = Math.max(maxActive, active);
        saves.push(value);
        await new Promise((r) => setTimeout(r, 20));
        active -= 1;
        return revision + 1;
      },
    });
    saver.reset("", 1);
    saver.update("a");
    await until(() => saver.getStatus() === "saving");
    saver.update("ab");
    await saver.flush();
    expect(saves).toEqual(["a", "ab"]);
    expect(maxActive).toBe(1);
    expect(saver.getStatus()).toBe("saved");
  });

  test("revision 冲突：状态变 conflict，不再重试，也不再自动保存", async () => {
    let n = 0;
    const seen: AutosaveStatus[] = [];
    const saver = new Autosaver({
      revision: 1,
      delay: 1,
      isConflict,
      onStatus: (s) => seen.push(s),
      save: async () => {
        n += 1;
        throw CONFLICT;
      },
    });
    saver.reset("a", 1);
    saver.update("ab");
    await until(() => saver.getStatus() === "conflict");
    saver.update("abc");
    await new Promise((r) => setTimeout(r, 20));
    expect(n).toBe(1);
    expect(saver.getStatus()).toBe("conflict");
  });

  test("加载最新后 reset 恢复自动保存", async () => {
    const saves: [string, number][] = [];
    let fail = true;
    const saver = new Autosaver({
      revision: 1,
      delay: 1,
      isConflict,
      save: async (v, r) => {
        if (fail) throw CONFLICT;
        saves.push([v, r]);
        return r + 1;
      },
    });
    saver.reset("a", 1);
    saver.update("b");
    await until(() => saver.getStatus() === "conflict");
    fail = false;
    saver.reset("服务端最新", 7);
    saver.update("服务端最新+我改的");
    await until(() => saves.length === 1);
    expect(saves[0]).toEqual(["服务端最新+我改的", 7]);
  });

  test("其他失败：状态 error，内容不丢，flush 可重试", async () => {
    let fail = true;
    const saver = new Autosaver({
      revision: 1,
      delay: 1,
      isConflict,
      save: async (_v, r) => {
        if (fail) throw new Error("网络");
        return r + 1;
      },
    });
    saver.reset("a", 1);
    saver.update("b");
    await until(() => saver.getStatus() === "error");
    fail = false;
    await saver.flush();
    expect(saver.getStatus()).toBe("saved");
    expect(saver.getRevision()).toBe(2);
  });
});
