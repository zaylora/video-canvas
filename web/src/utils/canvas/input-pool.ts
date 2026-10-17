/** 能释放的读取句柄：Mediabunny 的 Input 就是这样 */
type Disposable = { dispose: () => void };

/**
 * 没人用之后再留多久（毫秒）：截帧时一帧一帧地点，留着就能复用已经下载、解析过的数据，
 * 不用每截一帧都从头请求一遍视频。
 */
const IDLE_MS = 30_000;

type Entry<T> = {
  input: Promise<T>;
  /** 已经打开好的句柄：释放时能当场释放，不用再等一轮 Promise */
  opened?: T;
  /** 正在用它的读取数 */
  users: number;
  /** 等待释放的计时器 */
  timer?: unknown;
  /** 读取出过错：不再给后来的人用，没人用了立刻释放 */
  broken: boolean;
};

const isAbort = (error: unknown) => error instanceof DOMException && error.name === "AbortError";

/**
 * 按地址共用打开的视频：取时长、取缩略图、截帧先后读同一个视频时只打开一次，
 * Mediabunny 在 Input 里缓存的分段数据也就跟着复用。没人用了留 IDLE_MS 再释放。
 * 读取出错（断网、跨域）的句柄不再复用，免得一次失败之后一直失败；主动中止不算出错。
 * @param open 打开一个视频
 * @param options.setTimer、options.clearTimer 计时器，测试里注入手动推进的
 */
export function createInputPool<T extends Disposable>(
  open: (src: string) => Promise<T>,
  options: {
    idleMs?: number;
    setTimer?: (fn: () => void, ms: number) => unknown;
    clearTimer?: (id: unknown) => void;
  } = {},
) {
  const idleMs = options.idleMs ?? IDLE_MS;
  const setTimer = options.setTimer ?? ((fn, ms) => setTimeout(fn, ms));
  const clearTimer =
    options.clearTimer ?? ((id) => clearTimeout(id as ReturnType<typeof setTimeout>));
  const entries = new Map<string, Entry<T>>();

  const dispose = (entry: Entry<T>) => {
    if (entry.opened) entry.opened.dispose();
    else
      void entry.input.then(
        (input) => input.dispose(),
        () => {},
      );
  };

  const release = (src: string, entry: Entry<T>) => {
    entry.users--;
    if (entry.users > 0) return;
    if (entry.broken) {
      dispose(entry);
      return;
    }
    entry.timer = setTimer(() => {
      if (entries.get(src) === entry) entries.delete(src);
      dispose(entry);
    }, idleMs);
  };

  return {
    /** 用 src 对应的视频做一次读取；fn 结束（成功或失败）后归还 */
    async use<R>(src: string, fn: (input: T) => Promise<R>): Promise<R> {
      let entry = entries.get(src);
      if (!entry) {
        entry = { input: open(src), users: 0, broken: false };
        entries.set(src, entry);
      }
      if (entry.timer !== undefined) {
        clearTimer(entry.timer);
        entry.timer = undefined;
      }
      entry.users++;
      const current = entry;
      try {
        current.opened = await current.input;
        return await fn(current.opened);
      } catch (error) {
        if (!isAbort(error)) {
          current.broken = true;
          if (entries.get(src) === current) entries.delete(src);
        }
        throw error;
      } finally {
        release(src, current);
      }
    },
  };
}
