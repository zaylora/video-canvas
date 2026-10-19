/** 一张缩略图当前的状态 */
export type ThumbState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; url: string }
  | { status: "failed" };

/** 缩略图缓存 */
export type ThumbCache = {
  /** 当前状态；返回的对象在状态不变时是同一个引用，可以直接给 useSyncExternalStore */
  get: (key: string) => ThumbState;
  /**
   * 请求一张图：已经拍好直接给，正在拍的共用同一次，失败的会重新拍。
   * @returns 图片地址；拍失败是 null
   */
  request: (key: string) => Promise<string | null>;
  /** 状态变化时通知；返回取消订阅的函数 */
  subscribe: (listener: () => void) => () => void;
  /** 清空全部，之后要重新拍 */
  clear: () => void;
};

const IDLE: ThumbState = { status: "idle" };
const LOADING: ThumbState = { status: "loading" };
const FAILED: ThumbState = { status: "failed" };

/**
 * 缩略图缓存：资产面板里每张卡的预览图在浏览器里现拍，拍一次记下来。
 * 一个页面只有一个 WebGL 上下文拍图，所以请求排队一张一张来；某一张失败不挡后面的。
 * @param render 拍一张图，返回图片地址；拍不出来返回 null 或抛错，都算失败
 */
export function createThumbCache(render: (key: string) => Promise<string | null>): ThumbCache {
  const states = new Map<string, ThumbState>();
  const inflight = new Map<string, Promise<string | null>>();
  const listeners = new Set<() => void>();
  let queue: Promise<unknown> = Promise.resolve();

  const notify = () => listeners.forEach((listener) => listener());
  const set = (key: string, state: ThumbState) => {
    states.set(key, state);
    notify();
  };

  return {
    get: (key) => states.get(key) ?? IDLE,
    request: (key) => {
      const known = states.get(key);
      if (known?.status === "ready") return Promise.resolve(known.url);
      const running = inflight.get(key);
      if (running) return running;
      set(key, LOADING);
      const task = queue.then(async () => {
        try {
          const url = await render(key);
          set(key, url ? { status: "ready", url } : FAILED);
          return url;
        } catch {
          set(key, FAILED);
          return null;
        } finally {
          inflight.delete(key);
        }
      });
      inflight.set(key, task);
      queue = task;
      return task;
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    clear: () => {
      states.clear();
      notify();
    },
  };
}
