import { describe, expect, test } from "bun:test";

import { TrimError, type TrimEngine, type TrimProbe } from "@/utils/canvas/trim-run";
import {
  createTrimSourceCache,
  loadTrimSource,
  type TrimSourceState,
} from "@/utils/canvas/trim-source";

const probe: TrimProbe = { duration: 5, hasAudio: true, width: 1920, height: 1080 };

/** 假引擎：记下调用次数，缩略图按格子数返回 blob:1、blob:2… */
function fakeEngine(overrides: Partial<Pick<TrimEngine, "probe" | "thumbnails">> = {}) {
  const calls = { probe: 0, thumbnails: 0 };
  const engine: Pick<TrimEngine, "probe" | "thumbnails"> = {
    probe: async (...args) => {
      calls.probe++;
      return overrides.probe ? overrides.probe(...args) : probe;
    },
    thumbnails: async (...args) => {
      calls.thumbnails++;
      if (overrides.thumbnails) return overrides.thumbnails(...args);
      return args[1].map((_, index) => `blob:${index + 1}`);
    },
  };
  return { engine, calls };
}

/** 跑一次读取，收集每一步交给界面的状态 */
async function run(options: {
  engine: Pick<TrimEngine, "probe" | "thumbnails">;
  cache: ReturnType<typeof createTrimSourceCache>;
  signal?: AbortSignal;
  src?: string;
}) {
  const states: TrimSourceState[] = [];
  await loadTrimSource({
    src: options.src ?? "/files/a.mp4",
    engine: options.engine,
    cache: options.cache,
    signal: options.signal ?? new AbortController().signal,
    onState: (state) => states.push(state),
  });
  return states;
}

describe("loadTrimSource 读源视频（带缓存）", () => {
  test("第一次读：先给时长，再补缩略图；读的是修剪专用地址", async () => {
    const seen: string[] = [];
    const { engine } = fakeEngine({
      probe: async (src) => {
        seen.push(src);
        return probe;
      },
    });
    const states = await run({ engine, cache: createTrimSourceCache() });
    expect(seen).toEqual(["/files/a.mp4?trim=1"]);
    expect(states).toHaveLength(2);
    expect(states[0]).toEqual({ status: "ready", probe, thumbs: [] });
    expect(states[1].status === "ready" && states[1].thumbs.length).toBe(16);
  });

  test("再次打开同一个视频：直接用缓存，不再读", async () => {
    const cache = createTrimSourceCache();
    const first = fakeEngine();
    await run({ engine: first.engine, cache });

    const second = fakeEngine();
    const states = await run({ engine: second.engine, cache });
    expect(second.calls).toEqual({ probe: 0, thumbnails: 0 });
    expect(states).toHaveLength(1);
    expect(states[0].status === "ready" && states[0].thumbs[0]).toBe("blob:1");
  });

  test("上次缩略图没读完就关了：再打开只补缩略图，时长立刻可用", async () => {
    const cache = createTrimSourceCache();
    const controller = new AbortController();
    const first = fakeEngine({
      thumbnails: async () => {
        controller.abort();
        throw new DOMException("已中止", "AbortError");
      },
    });
    await run({ engine: first.engine, cache, signal: controller.signal });

    const second = fakeEngine();
    const states = await run({ engine: second.engine, cache });
    expect(second.calls).toEqual({ probe: 0, thumbnails: 1 });
    expect(states[0]).toEqual({ status: "ready", probe, thumbs: [] });
  });

  test("缩略图读失败：给一排占位，但不记进缓存，下次还会重试", async () => {
    const cache = createTrimSourceCache();
    const first = fakeEngine({
      thumbnails: async () => {
        throw new TypeError("Failed to fetch");
      },
    });
    const states = await run({ engine: first.engine, cache });
    const last = states.at(-1);
    expect(last?.status === "ready" && last.thumbs.every((url) => url === null)).toBe(true);

    const second = fakeEngine();
    await run({ engine: second.engine, cache });
    expect(second.calls).toEqual({ probe: 0, thumbnails: 1 });
  });

  test("读信息失败：落成 error，给写给用户的话，也不进缓存", async () => {
    const cache = createTrimSourceCache();
    const { engine } = fakeEngine({
      probe: async () => {
        throw new TrimError("no-video");
      },
    });
    const states = await run({ engine, cache });
    expect(states).toEqual([{ status: "error", message: new TrimError("no-video").message }]);
    expect(cache.get("/files/a.mp4?trim=1")).toBeUndefined();
  });

  test("中途中止：读完的缩略图释放掉，不交给界面", async () => {
    const revoked: string[] = [];
    const cache = createTrimSourceCache({ revoke: (url) => revoked.push(url) });
    const controller = new AbortController();
    const { engine } = fakeEngine({
      thumbnails: async (_src, times) => {
        controller.abort();
        return times.map((_, index) => `blob:${index + 1}`);
      },
    });
    const states = await run({ engine, cache, signal: controller.signal });
    expect(states).toHaveLength(1);
    expect(revoked).toHaveLength(16);
  });
});

describe("createTrimSourceCache 读取结果缓存", () => {
  const entry = (tag: string) => ({ probe, thumbs: [`blob:${tag}`, null] });

  test("超过上限淘汰最久没用的，并释放它的缩略图", () => {
    const revoked: string[] = [];
    const cache = createTrimSourceCache({ max: 2, revoke: (url) => revoked.push(url) });
    cache.set("a", entry("a"));
    cache.set("b", entry("b"));
    cache.get("a"); // 用过 a，最久没用的变成 b
    cache.set("c", entry("c"));
    expect(cache.get("b")).toBeUndefined();
    expect(cache.get("a")).toBeDefined();
    expect(revoked).toEqual(["blob:b"]);
  });

  test("同一个视频换了新缩略图：旧的释放掉", () => {
    const revoked: string[] = [];
    const cache = createTrimSourceCache({ revoke: (url) => revoked.push(url) });
    cache.set("a", entry("old"));
    cache.set("a", entry("new"));
    expect(revoked).toEqual(["blob:old"]);
    expect(cache.get("a")?.thumbs?.[0]).toBe("blob:new");
  });
});
