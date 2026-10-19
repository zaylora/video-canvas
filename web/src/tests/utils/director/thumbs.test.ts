import { describe, expect, test } from "bun:test";

import { createThumbCache } from "@/utils/director/thumbs";

/** 可以手动放行的假渲染：记录调用顺序和并发数 */
function fakeRenderer() {
  const calls: string[] = [];
  let running = 0;
  let peak = 0;
  const gates = new Map<string, () => void>();
  const render = (key: string) => {
    calls.push(key);
    running++;
    peak = Math.max(peak, running);
    return new Promise<string | null>((resolve) => {
      gates.set(key, () => {
        running--;
        resolve(key === "bad" ? null : `data:${key}`);
      });
    });
  };
  const release = (key: string) => gates.get(key)?.();
  return { render, calls, release, peak: () => peak };
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("缩略图缓存", () => {
  test("没请求过是 idle，请求后 loading，拍好是 ready 并能取到图", async () => {
    const r = fakeRenderer();
    const cache = createThumbCache(r.render);
    expect(cache.get("a")).toEqual({ status: "idle" });
    const done = cache.request("a");
    await tick();
    expect(cache.get("a").status).toBe("loading");
    r.release("a");
    expect(await done).toBe("data:a");
    expect(cache.get("a")).toEqual({ status: "ready", url: "data:a" });
  });

  test("同一张图重复请求只拍一次", async () => {
    const r = fakeRenderer();
    const cache = createThumbCache(r.render);
    const first = cache.request("a");
    const second = cache.request("a");
    await tick();
    r.release("a");
    await Promise.all([first, second]);
    await cache.request("a");
    expect(r.calls).toEqual(["a"]);
  });

  test("按顺序一张一张拍（只有一个 WebGL 上下文，不能并发）", async () => {
    const r = fakeRenderer();
    const cache = createThumbCache(r.render);
    const all = [cache.request("a"), cache.request("b"), cache.request("c")];
    await tick();
    expect(r.calls).toEqual(["a"]);
    r.release("a");
    await tick();
    expect(r.calls).toEqual(["a", "b"]);
    r.release("b");
    await tick();
    r.release("c");
    await Promise.all(all);
    expect(r.calls).toEqual(["a", "b", "c"]);
    expect(r.peak()).toBe(1);
  });

  test("拍失败记为 failed，不挡后面的；失败的可以重新请求", async () => {
    const r = fakeRenderer();
    const cache = createThumbCache(r.render);
    const bad = cache.request("bad");
    const good = cache.request("good");
    await tick();
    r.release("bad");
    await tick();
    r.release("good");
    expect(await bad).toBeNull();
    expect(await good).toBe("data:good");
    expect(cache.get("bad").status).toBe("failed");
    const retry = cache.request("bad");
    await tick();
    expect(r.calls.filter((k) => k === "bad")).toHaveLength(2);
    r.release("bad");
    await retry;
  });

  test("渲染函数抛错也记为 failed，不让队列卡死", async () => {
    const cache = createThumbCache(async (key) => {
      if (key === "boom") throw new Error("WebGL 上下文丢了");
      return `data:${key}`;
    });
    expect(await cache.request("boom")).toBeNull();
    expect(cache.get("boom").status).toBe("failed");
    expect(await cache.request("ok")).toBe("data:ok");
  });

  test("状态变化会通知订阅者，取消订阅后不再通知", async () => {
    const r = fakeRenderer();
    const cache = createThumbCache(r.render);
    let notified = 0;
    const off = cache.subscribe(() => notified++);
    const done = cache.request("a");
    await tick();
    r.release("a");
    await done;
    expect(notified).toBeGreaterThanOrEqual(2);
    off();
    const before = notified;
    const next = cache.request("b");
    await tick();
    r.release("b");
    await next;
    expect(notified).toBe(before);
  });

  test("清空缓存后要重新拍", async () => {
    const r = fakeRenderer();
    const cache = createThumbCache(r.render);
    const first = cache.request("a");
    await tick();
    r.release("a");
    await first;
    cache.clear();
    expect(cache.get("a").status).toBe("idle");
  });
});
