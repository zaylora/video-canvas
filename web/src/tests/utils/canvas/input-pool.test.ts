import { describe, expect, test } from "bun:test";

import { createInputPool } from "@/utils/canvas/input-pool";

/** 假的读取句柄：记下是哪个地址、有没有被释放 */
type FakeInput = { src: string; disposed: boolean; dispose: () => void };

/** 手动推进的计时器：run() 触发所有到点的回调 */
function fakeTimers() {
  let next = 1;
  const pending = new Map<number, () => void>();
  return {
    setTimer: (fn: () => void) => {
      const id = next++;
      pending.set(id, fn);
      return id;
    },
    clearTimer: (id: unknown) => void pending.delete(id as number),
    run: () => {
      const fns = [...pending.values()];
      pending.clear();
      fns.forEach((fn) => fn());
    },
    get size() {
      return pending.size;
    },
  };
}

function setup() {
  const opened: FakeInput[] = [];
  const timers = fakeTimers();
  const pool = createInputPool<FakeInput>(
    async (src) => {
      const input: FakeInput = {
        src,
        disposed: false,
        dispose() {
          this.disposed = true;
        },
      };
      opened.push(input);
      return input;
    },
    { setTimer: timers.setTimer, clearTimer: timers.clearTimer },
  );
  return { pool, opened, timers };
}

describe("createInputPool 共用打开的视频", () => {
  test("同一个地址先后几次读取，只打开一次", async () => {
    const { pool, opened } = setup();
    await pool.use("a", async () => {});
    await pool.use("a", async () => {});
    expect(opened).toHaveLength(1);
  });

  test("同时读取也共用同一个", async () => {
    const { pool, opened } = setup();
    await Promise.all([pool.use("a", async () => {}), pool.use("a", async () => {})]);
    expect(opened).toHaveLength(1);
  });

  test("不同地址各开各的", async () => {
    const { pool, opened } = setup();
    await pool.use("a", async () => {});
    await pool.use("b", async () => {});
    expect(opened.map((input) => input.src)).toEqual(["a", "b"]);
  });

  test("没人用了先留一会儿，到点才释放；释放后再读会重新打开", async () => {
    const { pool, opened, timers } = setup();
    await pool.use("a", async () => {});
    expect(opened[0].disposed).toBe(false);
    timers.run();
    expect(opened[0].disposed).toBe(true);
    await pool.use("a", async () => {});
    expect(opened).toHaveLength(2);
  });

  test("等待释放期间又有人用：取消释放", async () => {
    const { pool, opened, timers } = setup();
    await pool.use("a", async () => {});
    let during = false;
    await pool.use("a", async (input) => {
      during = timers.size === 0 && !input.disposed;
    });
    expect(during).toBe(true);
    expect(opened).toHaveLength(1);
  });

  test("还有人在用时不会被释放", async () => {
    const { pool, opened, timers } = setup();
    let finish = () => {};
    const long = pool.use("a", () => new Promise<void>((resolve) => (finish = resolve)));
    await pool.use("a", async () => {});
    timers.run();
    expect(opened[0].disposed).toBe(false);
    finish();
    await long;
    timers.run();
    expect(opened[0].disposed).toBe(true);
  });

  test("读取出错：这个句柄不再给后来的人用，下次重新打开", async () => {
    const { pool, opened } = setup();
    await expect(
      pool.use("a", async () => {
        throw new TypeError("Failed to fetch");
      }),
    ).rejects.toThrow("Failed to fetch");
    expect(opened[0].disposed).toBe(true);
    await pool.use("a", async () => {});
    expect(opened).toHaveLength(2);
  });

  test("中止不算出错：句柄留着继续复用", async () => {
    const { pool, opened } = setup();
    await expect(
      pool.use("a", async () => {
        throw new DOMException("已中止", "AbortError");
      }),
    ).rejects.toThrow();
    await pool.use("a", async () => {});
    expect(opened).toHaveLength(1);
    expect(opened[0].disposed).toBe(false);
  });

  test("打开失败：不留下坏条目，下次重试", async () => {
    let fail = true;
    const pool = createInputPool<FakeInput>(async (src) => {
      if (fail) throw new TypeError("Failed to fetch");
      return { src, disposed: false, dispose() {} };
    });
    await expect(pool.use("a", async () => {})).rejects.toThrow();
    fail = false;
    await expect(pool.use("a", async (input) => input.src)).resolves.toBe("a");
  });
});
