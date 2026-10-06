import { describe, expect, test } from "bun:test";

import { canvasLockName, watchCanvasLock, type LockLike } from "@/utils/canvas/canvas-lock";

const tick = async () => {
  for (let i = 0; i < 20; i += 1) await Promise.resolve();
};

/** 最小可用的 LockManager 假实现：同名锁互斥，ifAvailable 抢不到就给 null，排队的请求可用 signal 取消 */
function fakeLocks(): LockLike {
  const holders = new Set<string>();
  const queue: {
    name: string;
    cb: (lock: unknown) => Promise<unknown>;
    resolve: (value: unknown) => void;
    reject: (reason: unknown) => void;
  }[] = [];

  const run = (name: string, cb: (lock: unknown) => Promise<unknown>) =>
    new Promise<unknown>((resolve, reject) => {
      holders.add(name);
      cb({ name })
        .then(resolve, reject)
        .finally(() => {
          holders.delete(name);
          const index = queue.findIndex((item) => item.name === name);
          if (index >= 0) {
            const [next] = queue.splice(index, 1);
            run(name, next.cb).then(next.resolve, next.reject);
          }
        });
    });

  return {
    request(name, options, callback) {
      if (options.ifAvailable) {
        return holders.has(name) ? callback(null) : run(name, callback);
      }
      if (!holders.has(name)) return run(name, callback);
      return new Promise<unknown>((resolve, reject) => {
        const item = { name, cb: callback, resolve, reject };
        queue.push(item);
        options.signal?.addEventListener("abort", () => {
          const index = queue.indexOf(item);
          if (index >= 0) {
            queue.splice(index, 1);
            reject(new DOMException("aborted", "AbortError"));
          }
        });
      });
    },
  };
}

describe("canvasLockName", () => {
  test("按用户和画布区分，不同画布、不同账号互不影响", () => {
    expect(canvasLockName("7", "c1")).toBe("canvas:7:c1");
    expect(canvasLockName("7", "c1")).not.toBe(canvasLockName("8", "c1"));
  });
});

describe("watchCanvasLock", () => {
  test("没人占用：拿到锁，可以编辑", async () => {
    const locks = fakeLocks();
    const states: string[] = [];
    watchCanvasLock({ name: "canvas:7:c1", locks, onChange: (s) => states.push(s) });
    await tick();
    expect(states).toEqual(["held"]);
  });

  test("已被另一个标签页占用：等待，提示只读", async () => {
    const locks = fakeLocks();
    const first: string[] = [];
    const second: string[] = [];
    watchCanvasLock({ name: "canvas:7:c1", locks, onChange: (s) => first.push(s) });
    await tick();
    watchCanvasLock({ name: "canvas:7:c1", locks, onChange: (s) => second.push(s) });
    await tick();
    expect(first).toEqual(["held"]);
    expect(second).toEqual(["waiting"]);
  });

  test("先开的标签页关闭后，后开的自动接管", async () => {
    const locks = fakeLocks();
    const second: string[] = [];
    const stopFirst = watchCanvasLock({ name: "canvas:7:c1", locks, onChange: () => {} });
    await tick();
    watchCanvasLock({ name: "canvas:7:c1", locks, onChange: (s) => second.push(s) });
    await tick();
    stopFirst();
    await tick();
    expect(second).toEqual(["waiting", "held"]);
  });

  test("等待中就离开：之后锁被释放也不会再通知，更不会抢走锁", async () => {
    const locks = fakeLocks();
    const second: string[] = [];
    const stopFirst = watchCanvasLock({ name: "canvas:7:c1", locks, onChange: () => {} });
    await tick();
    const stopSecond = watchCanvasLock({
      name: "canvas:7:c1",
      locks,
      onChange: (s) => second.push(s),
    });
    await tick();
    stopSecond();
    stopFirst();
    await tick();
    expect(second).toEqual(["waiting"]);
    // 锁已经空出来：新开的标签页能直接拿到
    const third: string[] = [];
    watchCanvasLock({ name: "canvas:7:c1", locks, onChange: (s) => third.push(s) });
    await tick();
    expect(third).toEqual(["held"]);
  });

  test("不同画布互不影响", async () => {
    const locks = fakeLocks();
    const other: string[] = [];
    watchCanvasLock({ name: "canvas:7:c1", locks, onChange: () => {} });
    await tick();
    watchCanvasLock({ name: "canvas:7:c2", locks, onChange: (s) => other.push(s) });
    await tick();
    expect(other).toEqual(["held"]);
  });

  test("浏览器不支持 Web Locks：不拦着用户，直接当作拿到", async () => {
    const states: string[] = [];
    watchCanvasLock({ name: "canvas:7:c1", locks: undefined, onChange: (s) => states.push(s) });
    await tick();
    expect(states).toEqual(["held"]);
  });

  test("锁请求本身抛错：同样不拦着用户", async () => {
    const states: string[] = [];
    const locks: LockLike = {
      request: () => Promise.reject(new Error("SecurityError")),
    };
    watchCanvasLock({ name: "canvas:7:c1", locks, onChange: (s) => states.push(s) });
    await tick();
    expect(states).toEqual(["held"]);
  });
});
