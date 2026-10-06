/**
 * 视口是「这台设备此刻看哪里」，属于视图状态，不是画布内容，所以不走保存链路：
 * 单独存在 localStorage，几十字节，同步写入，页面销毁时也一定写得完。
 * 所有方法都不抛错：隐私模式下 localStorage 可能不可用，调用方退回云端里的视口。
 */
export type StoredViewport = { x: number; y: number; zoom: number };

const keyOf = (userId: string, canvasId: string) => `viewport:${userId}:${canvasId}`;

const defaultStorage = () => {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
};

function isViewport(value: unknown): value is StoredViewport {
  if (!value || typeof value !== "object") return false;
  const { x, y, zoom } = value as Partial<StoredViewport>;
  return (
    typeof x === "number" &&
    Number.isFinite(x) &&
    typeof y === "number" &&
    Number.isFinite(y) &&
    typeof zoom === "number" &&
    Number.isFinite(zoom) &&
    zoom > 0
  );
}

/** 读本机视口；没有、被改坏、存储不可用都返回 null */
export function loadViewport(
  userId: string,
  canvasId: string,
  storage: Storage | undefined = defaultStorage(),
): StoredViewport | null {
  try {
    const raw = storage?.getItem(keyOf(userId, canvasId));
    if (!raw) return null;
    const value: unknown = JSON.parse(raw);
    return isViewport(value) ? { x: value.x, y: value.y, zoom: value.zoom } : null;
  } catch {
    return null;
  }
}

export function saveViewport(
  userId: string,
  canvasId: string,
  viewport: StoredViewport,
  storage: Storage | undefined = defaultStorage(),
) {
  try {
    storage?.setItem(keyOf(userId, canvasId), JSON.stringify(viewport));
  } catch {
    // 存不了就算了，下次打开用云端里的视口
  }
}

/** 删掉某一张画布的视口记录（画布被删除时用） */
export function removeViewport(
  userId: string,
  canvasId: string,
  storage: Storage | undefined = defaultStorage(),
) {
  try {
    storage?.removeItem(keyOf(userId, canvasId));
  } catch {
    // 删不掉不影响删除画布
  }
}

/** 清掉这个用户的全部视口记录（退出登录用）；前缀带冒号，user 7 不会误删 user 70 */
export function clearUserViewports(
  userId: string,
  storage: Storage | undefined = defaultStorage(),
) {
  try {
    if (!storage) return;
    const prefix = `viewport:${userId}:`;
    const keys: string[] = [];
    for (let i = 0; i < storage.length; i += 1) {
      const key = storage.key(i);
      if (key?.startsWith(prefix)) keys.push(key);
    }
    keys.forEach((key) => storage.removeItem(key));
  } catch {
    // 清不掉不影响退出
  }
}

/** 两个视口是否相同，容忍浮点误差：打开画布时恢复视口那一下不能被当成用户移动 */
export function sameViewport(a: StoredViewport, b: StoredViewport) {
  const near = (left: number, right: number) => Math.abs(left - right) < 1e-6;
  return near(a.x, b.x) && near(a.y, b.y) && near(a.zoom, b.zoom);
}

type Timers = {
  setTimer: (fn: () => void, ms: number) => unknown;
  clearTimer: (id: unknown) => void;
};

const realTimers: Timers = {
  setTimer: (fn, ms) => window.setTimeout(fn, ms),
  clearTimer: (id) => window.clearTimeout(id as number),
};

/**
 * 防抖写入：平移缩放时每次移动都重新计时，停下 delayMs 后只写最后一次。
 * flush 用于页面隐藏、离开画布时把还没写的立刻写出去。
 */
export function createViewportWriter(
  write: (viewport: StoredViewport) => void,
  delayMs = 200,
  timers: Timers = realTimers,
) {
  let pending: StoredViewport | null = null;
  let timer: unknown = null;

  const flush = () => {
    if (timer !== null) timers.clearTimer(timer);
    timer = null;
    if (!pending) return;
    const viewport = pending;
    pending = null;
    write(viewport);
  };

  return {
    schedule(viewport: StoredViewport) {
      pending = viewport;
      if (timer !== null) timers.clearTimer(timer);
      timer = timers.setTimer(flush, delayMs);
    },
    flush,
  };
}
