/** 只用到 Web Locks 的这一个重载，方便测试里换成假实现 */
export type LockLike = {
  request: (
    name: string,
    options: { ifAvailable?: boolean; signal?: AbortSignal },
    callback: (lock: unknown) => Promise<unknown>,
  ) => Promise<unknown>;
};

export type CanvasLockState = "held" | "waiting";

/** 锁名按用户和画布区分：不同画布、不同账号互不影响 */
export const canvasLockName = (userId: string, canvasId: string) => `canvas:${userId}:${canvasId}`;

/**
 * 同一张画布同一时刻只让一个标签页编辑，免得两个标签页互相覆盖本地草稿：
 * - 拿到锁：held，可以编辑
 * - 已被另一个标签页占用：waiting，调用方提示「已在另一个标签页打开」，同时排队等锁
 * - 占用的标签页关闭（锁自动释放）：排队的自动接管，变成 held
 * 浏览器不支持 Web Locks 或请求出错时不拦着用户，直接当作 held。返回停止函数：释放锁、取消排队。
 * waitingDelayMs：抢不到锁后先等这么久再通知 waiting。刚离开画布的标签页释放锁是异步的，
 * 马上在新标签页打开同一张画布会短暂抢不到，宽限期内锁空出来就直接 held，不让提示一闪而过。
 */
export function watchCanvasLock({
  name,
  locks,
  onChange,
  waitingDelayMs = 0,
}: {
  name: string;
  locks: LockLike | undefined;
  onChange: (state: CanvasLockState) => void;
  waitingDelayMs?: number;
}) {
  let stopped = false;
  let waitingTimer: ReturnType<typeof setTimeout> | undefined;
  let release: (() => void) | null = null;
  const controller = new AbortController();

  const notify = (state: CanvasLockState) => {
    if (!stopped) onChange(state);
  };
  /** 通知 waiting：有宽限就等宽限过了还在等才通知 */
  const notifyWaiting = () => {
    if (waitingDelayMs <= 0) notify("waiting");
    else waitingTimer = setTimeout(() => notify("waiting"), waitingDelayMs);
  };
  /** 拿到锁后一直占着，直到停止 */
  const hold = () =>
    new Promise<void>((resolve) => {
      release = resolve;
      if (stopped) resolve();
    });
  const open = () => notify("held");

  if (!locks) {
    open();
    return () => {
      stopped = true;
    };
  }

  locks
    .request(name, { ifAvailable: true }, async (lock) => {
      if (lock) {
        notify("held");
        await hold();
        return;
      }
      notifyWaiting();
      // 排队等占用的标签页关闭
      await locks
        .request(name, { signal: controller.signal }, async () => {
          clearTimeout(waitingTimer);
          notify("held");
          await hold();
        })
        .catch(() => undefined);
    })
    .catch(open);

  return () => {
    stopped = true;
    clearTimeout(waitingTimer);
    controller.abort();
    release?.();
  };
}
