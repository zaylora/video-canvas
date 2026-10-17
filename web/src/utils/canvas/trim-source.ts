import { thumbTimes } from "./trim";
import { describeTrimError, type TrimEngine, type TrimProbe } from "./trim-run";

/** 胶片条的缩略图格数：宽度大约 640px，每格约 40px 宽 */
const THUMB_COUNT = 16;
/** 缩略图高度（像素）：胶片条高 48，取两倍给高分屏 */
const THUMB_HEIGHT = 96;
/** 最多记住几个视频的读取结果：每个是 16 张小 JPEG，加起来也就几 MB */
const MAX_ENTRIES = 8;

/** 修剪、截帧面板读源视频的状态 */
export type TrimSourceState =
  | { status: "loading" }
  | { status: "ready"; probe: TrimProbe; thumbs: (string | null)[] }
  | { status: "error"; message: string };

/** 一个视频读过的结果：时长等信息，和胶片条缩略图（还没读完时没有） */
export type TrimSourceEntry = { probe: TrimProbe; thumbs?: (string | null)[] };

/**
 * 读取结果的缓存，按取流地址记：再打开同一个视频的修剪或截帧面板时直接用，不再从头读。
 * 缩略图的 object URL 归缓存管，被淘汰或被新结果替换时才释放；超过上限淘汰最久没用的。
 * @param options.revoke 释放 object URL，测试里注入
 */
export function createTrimSourceCache(
  options: { max?: number; revoke?: (url: string) => void } = {},
) {
  const max = options.max ?? MAX_ENTRIES;
  const revoke = options.revoke ?? ((url: string) => URL.revokeObjectURL(url));
  const entries = new Map<string, TrimSourceEntry>();
  const release = (thumbs: TrimSourceEntry["thumbs"]) => {
    for (const url of thumbs ?? []) if (url) revoke(url);
  };

  return {
    revoke,
    /** 取一条，并把它记成最近用过 */
    get(src: string): TrimSourceEntry | undefined {
      const entry = entries.get(src);
      if (entry) {
        entries.delete(src);
        entries.set(src, entry);
      }
      return entry;
    },
    set(src: string, entry: TrimSourceEntry) {
      const old = entries.get(src);
      if (old && old.thumbs !== entry.thumbs) release(old.thumbs);
      entries.delete(src);
      entries.set(src, entry);
      for (const [key, oldest] of entries) {
        if (entries.size <= max) break;
        entries.delete(key);
        release(oldest.thumbs);
      }
    },
  };
}

export type TrimSourceCache = ReturnType<typeof createTrimSourceCache>;

/** 页面里共用的一份缓存 */
export const trimSourceCache = createTrimSourceCache();

/** 缓存里已有完整结果时直接给出状态：面板初次渲染就用它，再打开时不闪一下「加载中」 */
export function cachedTrimSource(src: string, cache: TrimSourceCache): TrimSourceState | undefined {
  const entry = cache.get(src);
  return entry?.thumbs ? { status: "ready", probe: entry.probe, thumbs: entry.thumbs } : undefined;
}

/**
 * 读源视频：时长、有没有音轨、胶片条缩略图。先读信息（快），拿到就交出 ready，缩略图随后补上。
 * 读过的结果记进缓存：完整的直接用，只有信息没有缩略图（上次没读完就关了）的只补缩略图。
 * 缩略图读失败给一排 null，但不记进缓存，下次打开还会重试；中止时什么都不交出，读完的缩略图释放掉。
 * @param input.onState 每一步交给界面的状态
 */
export async function loadTrimSource(input: {
  src: string;
  engine: Pick<TrimEngine, "probe" | "thumbnails">;
  cache: TrimSourceCache;
  signal: AbortSignal;
  onState: (state: TrimSourceState) => void;
}): Promise<void> {
  const { engine, cache, signal, onState } = input;
  const read = input.src;
  const cached = cache.get(read);
  if (cached?.thumbs) {
    onState({ status: "ready", probe: cached.probe, thumbs: cached.thumbs });
    return;
  }

  try {
    const probe = cached?.probe ?? (await engine.probe(read, signal));
    if (signal.aborted) return;
    if (!cached) cache.set(read, { probe });
    onState({ status: "ready", probe, thumbs: [] });

    let failed = false;
    const thumbs = await engine
      .thumbnails(read, thumbTimes(probe.duration, THUMB_COUNT), THUMB_HEIGHT, signal)
      .catch(() => {
        failed = true;
        return Array.from({ length: THUMB_COUNT }, () => null) as (string | null)[];
      });
    if (signal.aborted) {
      if (!failed) for (const url of thumbs) if (url) cache.revoke(url);
      return;
    }
    if (!failed) cache.set(read, { probe, thumbs });
    onState({ status: "ready", probe, thumbs });
  } catch (error) {
    if (signal.aborted) return;
    onState({ status: "error", message: describeTrimError(error) });
  }
}
