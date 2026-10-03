import { useCallback, useEffect, useRef, useState } from "react";
import type { CanvasDetailDto, CanvasGraphDto } from "@/api/canvas/type";
import { getCanvas, saveCanvasGraph, updateCanvas } from "@/api/canvas";
import { canKeepalive, nextSaveDelay } from "@/utils/canvas/save-schedule";
import { ApiError } from "@/utils/requests/request";

/**
 * - dirty：有改动，还在等停手，请求没发出去
 * - saving：请求在途
 * - conflict：别处改过了，等用户在弹窗里选怎么处理
 */
export type SaveStatus = "loading" | "saved" | "dirty" | "saving" | "error" | "conflict";

/** 保存失败后的自动重试间隔，用完了就停在 error 等用户手动点 */
const RETRY_DELAYS_MS = [5000, 15_000, 30_000];

/**
 * 画布保存：改动只标脏，停手一会儿（或连续编辑到最长等待）才真正发请求。
 * - 图谱不在每次改动时序列化，发请求那一刻才通过 getGraph 取最新的，视口也就跟着带上，平移缩放本身不触发保存
 * - 同一时刻最多一个在途请求；落地后若又有新改动，按停手窗口重新排期，不会立刻补发
 * - 页面隐藏、关闭、离开画布时立即保存；409 不再静默丢内容，交给调用方弹窗让用户选
 */
export function useCanvasPersistence({
  canvasId,
  initialVersion,
  getGraph,
  onConflict,
}: {
  canvasId: string;
  initialVersion: number;
  /** 取当前完整图谱；还没准备好时返回 null */
  getGraph: () => CanvasGraphDto | null;
  /** 用户选择「加载最新」后，把别处的最新画布交给外面重挂 */
  onConflict: (current: CanvasDetailDto) => void;
}) {
  const [status, setStatus] = useState<SaveStatus>("saved");
  /** 保存撞上 409 时拉回来的最新画布；非空期间不再自动保存 */
  const [conflict, setConflict] = useState<CanvasDetailDto | null>(null);
  const versionRef = useRef(initialVersion);
  const dirtyRef = useRef(false);
  const firstDirtyAtRef = useRef(0);
  const lastChangeAtRef = useRef(0);
  const flightRef = useRef<Promise<unknown> | null>(null);
  const conflictRef = useRef<CanvasDetailDto | null>(null);
  const retryRef = useRef(0);
  const timerRef = useRef<number | null>(null);
  const activeRef = useRef(true);
  // 回调放进 ref：flush 的身份不随外面重渲染变，卸载时的 flush 才不会在画布使用中途误触发
  const getGraphRef = useRef(getGraph);
  const onConflictRef = useRef(onConflict);
  useEffect(() => {
    getGraphRef.current = getGraph;
    onConflictRef.current = onConflict;
  }, [getGraph, onConflict]);

  const clearTimer = useCallback(() => {
    if (timerRef.current === null) return;
    window.clearTimeout(timerRef.current);
    timerRef.current = null;
  }, []);

  const show = useCallback((next: SaveStatus) => {
    if (activeRef.current) setStatus(next);
  }, []);

  /** 拉最新画布放进冲突态；拉不到就当保存失败，本地内容还在 */
  const enterConflict = useCallback(async () => {
    try {
      const current = await getCanvas(canvasId);
      conflictRef.current = current;
      clearTimer();
      if (activeRef.current) {
        setConflict(current);
        setStatus("conflict");
      }
      return true;
    } catch {
      return false;
    }
  }, [canvasId, clearTimer]);

  /** 独占一次请求：排在在途的后面，保证改名和图谱保存共用的 revision 不会撞车 */
  const exclusive = useCallback(async <T>(task: () => Promise<T>): Promise<T> => {
    while (flightRef.current) await flightRef.current.catch(() => undefined);
    const mine = task();
    flightRef.current = mine;
    try {
      return await mine;
    } finally {
      if (flightRef.current === mine) flightRef.current = null;
    }
  }, []);

  /** save 要在 schedule 之后才定义，两边互相调用，借 ref 中转 */
  const saveRef = useRef<(keepalive?: boolean) => Promise<boolean>>(async () => true);

  const schedule = useCallback(() => {
    clearTimer();
    if (flightRef.current || conflictRef.current || !dirtyRef.current) return;
    const delay = nextSaveDelay({
      now: Date.now(),
      firstDirtyAt: firstDirtyAtRef.current,
      lastChangeAt: lastChangeAtRef.current,
    });
    timerRef.current = window.setTimeout(() => void saveRef.current(), delay);
  }, [clearTimer]);

  /**
   * 把当前内容存一次。调用前保证没有在途请求；返回「现在没有未保存的内容了」。
   * keepalive 给页面隐藏、关闭用：请求在页面销毁后也能发完，超过体积上限自动退回普通请求。
   */
  const save = useCallback(
    async (keepalive = false) => {
      if (!dirtyRef.current || conflictRef.current) return !dirtyRef.current;
      const graph = getGraphRef.current();
      if (!graph) return false;

      clearTimer();
      // 这一批改动交给本次请求；请求期间新来的改动会重新标脏
      const batchStart = firstDirtyAtRef.current;
      dirtyRef.current = false;
      show("saving");
      try {
        const saved = await exclusive(() =>
          saveCanvasGraph(
            canvasId,
            { baseVersion: versionRef.current, graph },
            {
              silent: true,
              ...(keepalive && canKeepalive(graph)
                ? { adapter: "fetch" as const, fetchOptions: { keepalive: true } }
                : {}),
            },
          ),
        );
        versionRef.current = saved.version;
        retryRef.current = 0;
        if (dirtyRef.current) {
          show("dirty");
          schedule();
          return false;
        }
        show("saved");
        return true;
      } catch (error) {
        // 内容没存上：放回脏态，下次保存发的是最新的全量
        if (!dirtyRef.current) firstDirtyAtRef.current = batchStart;
        else firstDirtyAtRef.current = Math.min(firstDirtyAtRef.current, batchStart);
        dirtyRef.current = true;
        if (error instanceof ApiError && error.status === 409 && (await enterConflict())) {
          return false;
        }
        show("error");
        // 自动重试几次；之后停下来，状态栏上点「重试」或联网事件再触发
        const delay = RETRY_DELAYS_MS[retryRef.current];
        if (delay !== undefined && !conflictRef.current) {
          retryRef.current += 1;
          clearTimer();
          timerRef.current = window.setTimeout(() => void saveRef.current(), delay);
        }
        return false;
      }
    },
    [canvasId, clearTimer, enterConflict, exclusive, schedule, show],
  );
  useEffect(() => {
    saveRef.current = save;
  }, [save]);

  /**
   * 立即保存：等在途请求落地再发，返回保存后是否没有未保存内容。
   * 手动保存、点重试、离开画布前用；失败或有冲突时返回 false。
   */
  const flush = useCallback(
    async ({ keepalive = false }: { keepalive?: boolean } = {}) => {
      while (flightRef.current) await flightRef.current.catch(() => undefined);
      retryRef.current = 0;
      return save(keepalive);
    },
    [save],
  );

  /** 内容有改动：只标脏并排期，真正发请求在停手之后 */
  const changed = useCallback(() => {
    const now = Date.now();
    if (!dirtyRef.current) firstDirtyAtRef.current = now;
    dirtyRef.current = true;
    lastChangeAtRef.current = now;
    if (conflictRef.current) return;
    // 请求在途时保持「保存中」，落地后若还有改动再转「未保存」
    if (!flightRef.current) show("dirty");
    schedule();
  }, [schedule, show]);

  /** 用户选了「加载最新」或「另存为」之后：把冲突态清掉，本地未保存的内容不再提交 */
  const dismissConflict = useCallback(() => {
    conflictRef.current = null;
    dirtyRef.current = false;
    setConflict(null);
  }, []);

  /**
   * 改画布名：和图谱保存共用同一个 revision，排在在途保存后面再发，
   * 改完把新 revision 接过来，后面的图谱保存不会撞 409。
   */
  const rename = useCallback(
    async (title: string) => {
      if (conflictRef.current) return false;
      try {
        const saved = await exclusive(() =>
          updateCanvas(canvasId, { revision: versionRef.current, title }, { silent: true }),
        );
        versionRef.current = saved.version;
        return true;
      } catch (error) {
        if (error instanceof ApiError && error.status === 409) await enterConflict();
        return false;
      } finally {
        schedule();
      }
    },
    [canvasId, enterConflict, exclusive, schedule],
  );

  useEffect(() => {
    activeRef.current = true;
    const leaving = () => void flush({ keepalive: true });
    const onVisibility = () => {
      if (document.visibilityState === "hidden") leaving();
    };
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      // 还有没存上的内容（含请求在途、失败、冲突）：让浏览器弹确认
      if (!dirtyRef.current && !flightRef.current) return;
      event.preventDefault();
    };
    // 断网后联网：失败状态下立刻再试一次
    const onOnline = () => {
      if (dirtyRef.current && !conflictRef.current) void flush();
    };
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pagehide", leaving);
    window.addEventListener("beforeunload", onBeforeUnload);
    window.addEventListener("online", onOnline);
    return () => {
      // 路由跳转离开画布：把还没存的内容发出去。有冲突时不存，免得又撞一次
      void flush();
      activeRef.current = false;
      clearTimer();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", leaving);
      window.removeEventListener("beforeunload", onBeforeUnload);
      window.removeEventListener("online", onOnline);
    };
  }, [clearTimer, flush]);

  return { status, conflict, changed, flush, rename, dismissConflict };
}
