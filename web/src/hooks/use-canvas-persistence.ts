import { useCallback, useEffect, useRef, useState } from "react";
import type { CanvasDetailDto, CanvasGraphDto } from "@/api/canvas/type";
import { getCanvas, saveCanvasGraph, updateCanvas } from "@/api/canvas";
import { ApiError } from "@/utils/requests/request";

export type SaveStatus = "loading" | "saved" | "saving" | "error" | "conflict";

export function useCanvasPersistence({
  canvasId,
  initialVersion,
  onConflict,
}: {
  canvasId: string;
  initialVersion: number;
  onConflict: (current: CanvasDetailDto) => void;
}) {
  const [status, setStatus] = useState<SaveStatus>("saved");
  const versionRef = useRef(initialVersion);
  const revisionRef = useRef(0);
  const savedRevisionRef = useRef(0);
  const inFlightRef = useRef(false);
  const latestGraphRef = useRef<CanvasGraphDto | null>(null);
  const timerRef = useRef<number | null>(null);
  const activeRef = useRef(true);
  const flushRef = useRef<() => Promise<void>>(async () => undefined);

  const flush = useCallback(async () => {
    if (
      inFlightRef.current ||
      !latestGraphRef.current ||
      revisionRef.current === savedRevisionRef.current
    )
      return;
    inFlightRef.current = true;
    setStatus("saving");
    const revision = revisionRef.current;
    const graph = latestGraphRef.current;
    try {
      const saved = await saveCanvasGraph(canvasId, { baseVersion: versionRef.current, graph });
      if (!activeRef.current) return;
      versionRef.current = saved.version;
      savedRevisionRef.current = revision;
      if (revisionRef.current === revision) setStatus("saved");
      else window.setTimeout(() => void flushRef.current(), 0);
    } catch (error) {
      if (!activeRef.current) return;
      if (error instanceof ApiError && error.status === 409) {
        try {
          const current = await getCanvas(canvasId);
          if (!activeRef.current) return;
          revisionRef.current = 0;
          savedRevisionRef.current = 0;
          versionRef.current = current.version;
          latestGraphRef.current = null;
          setStatus("conflict");
          onConflict(current);
          return;
        } catch {
          // 冲突后的详情查询也失败时，保留本地内容并提示保存失败。
        }
      }
      setStatus("error");
    } finally {
      inFlightRef.current = false;
    }
  }, [canvasId, onConflict]);
  useEffect(() => {
    flushRef.current = flush;
  }, [flush]);

  /** 409 时拉最新的画布交给调用方重挂；拉取也失败就报保存失败 */
  const resolveConflict = useCallback(async () => {
    try {
      const current = await getCanvas(canvasId);
      if (!activeRef.current) return true;
      revisionRef.current = 0;
      savedRevisionRef.current = 0;
      versionRef.current = current.version;
      latestGraphRef.current = null;
      setStatus("conflict");
      onConflict(current);
      return true;
    } catch {
      return false;
    }
  }, [canvasId, onConflict]);

  /**
   * 改画布名：和图谱保存共用同一个 revision，所以等在路上的那次保存落地再发，
   * 改完把新 revision 接过来，后面的图谱保存不会撞 409。
   */
  const rename = useCallback(
    async (title: string) => {
      for (let i = 0; inFlightRef.current && i < 50; i++) {
        await new Promise((resolve) => window.setTimeout(resolve, 100));
      }
      inFlightRef.current = true;
      try {
        const saved = await updateCanvas(canvasId, { revision: versionRef.current, title });
        if (activeRef.current) versionRef.current = saved.version;
        return true;
      } catch (error) {
        if (error instanceof ApiError && error.status === 409) await resolveConflict();
        return false;
      } finally {
        inFlightRef.current = false;
        if (revisionRef.current !== savedRevisionRef.current) void flushRef.current();
      }
    },
    [canvasId, resolveConflict],
  );

  const changed = useCallback(
    (graph: CanvasGraphDto, delay = 800) => {
      latestGraphRef.current = graph;
      revisionRef.current += 1;
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
      timerRef.current = window.setTimeout(() => void flush(), delay);
    },
    [flush],
  );

  useEffect(() => {
    activeRef.current = true;
    const onVisibility = () => {
      if (document.visibilityState === "hidden") void flush();
    };
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (revisionRef.current === savedRevisionRef.current) return;
      event.preventDefault();
    };
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      activeRef.current = false;
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("beforeunload", onBeforeUnload);
    };
  }, [flush]);

  return { status, changed, flush, rename };
}
