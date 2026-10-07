import { useCallback, useEffect, useRef, useState } from "react";
import type { CanvasDetailDto, CanvasGraphDto } from "@/api/canvas/type";
import { getCanvas, saveCanvasGraph, updateCanvas } from "@/api/canvas";
import { draftStore } from "@/utils/canvas/draft-idb";
import { registerExitFlush } from "@/utils/canvas/exit-flush";
import { SaveCoordinator } from "@/utils/canvas/save-coordinator";
import { canKeepalive } from "@/utils/canvas/save-schedule";
import type { SaveStatus } from "@/utils/canvas/save-status";
import { ApiError } from "@/utils/requests/request";
import { getCurrentUserId } from "@/utils/storage/user-id";

export type { SaveStatus };

/**
 * 画布保存的 React 封装，调度逻辑都在 SaveCoordinator 里：
 * - 本地草稿：改动后停手 0.3 秒写进 IndexedDB，关页、断网、崩溃都不丢
 * - 云端：停手 3 秒（最长 10 秒）上传，成功后删草稿；失败自动重试 3 次
 * - 图谱不在每次改动时序列化，写草稿和发请求那一刻才通过 getGraph 取最新的
 * - 页面隐藏、关闭、离开画布时立即写草稿并尝试上传；409 不静默丢内容，交给调用方弹窗让用户选
 * - 视口不走这里：它是视图状态，由画布页单独存本机
 */
export function useCanvasPersistence({
  canvasId,
  initialVersion,
  getGraph,
  onConflict,
  autoMerge,
  onSaved,
}: {
  canvasId: string;
  initialVersion: number;
  /** 取当前完整图谱；还没准备好时返回 null */
  getGraph: () => CanvasGraphDto | null;
  /** 用户选择「加载最新」后，把别处的最新画布交给外面重挂 */
  onConflict: (current: CanvasDetailDto) => void;
  /** 保存撞上 409 时先试着自动合并：返回合并后的服务端版本号，null 表示不能合并，走冲突弹窗 */
  autoMerge?: () => Promise<number | null>;
  /** 一次保存成功：这份图谱就是服务端在该版本的内容 */
  onSaved?: (graph: CanvasGraphDto, version: number) => void;
}) {
  const [status, setStatus] = useState<SaveStatus>("saved");
  /** 保存撞上 409 时拉回来的最新画布；非空期间不再自动上传 */
  const [conflict, setConflict] = useState<CanvasDetailDto | null>(null);
  // 回调放进 ref：flush 的身份不随外面重渲染变，卸载时的 flush 才不会在画布使用中途误触发
  const getGraphRef = useRef(getGraph);
  const onConflictRef = useRef(onConflict);
  const autoMergeRef = useRef(autoMerge);
  const onSavedRef = useRef(onSaved);
  useEffect(() => {
    getGraphRef.current = getGraph;
    onConflictRef.current = onConflict;
    autoMergeRef.current = autoMerge;
    onSavedRef.current = onSaved;
  }, [getGraph, onConflict, autoMerge, onSaved]);

  const [coordinator] = useState(() => {
    // 认不出当前用户就没有草稿：退化为只走云端，行为和以前一致
    const userId = getCurrentUserId();
    return new SaveCoordinator({
      initialVersion,
      getGraph: () => getGraphRef.current(),
      saveCloud: ({ baseVersion, graph, keepalive }) =>
        saveCanvasGraph(
          canvasId,
          { baseVersion, graph },
          {
            silent: true,
            // keepalive 给页面隐藏、关闭用：请求在页面销毁后也能发完，超过体积上限自动退回普通请求
            ...(keepalive && canKeepalive(graph)
              ? { adapter: "fetch" as const, fetchOptions: { keepalive: true } }
              : {}),
          },
        ),
      isConflict: (error) => error instanceof ApiError && error.status === 409,
      autoMerge: () => autoMergeRef.current?.() ?? Promise.resolve(null),
      onSaved: (graph, version) => onSavedRef.current?.(graph, version),
      onConflict: async () => {
        try {
          setConflict(await getCanvas(canvasId));
          return true;
        } catch {
          return false;
        }
      },
      draft: userId
        ? {
            save: (baseVersion, graph) =>
              draftStore.save(userId, canvasId, { graph, baseVersion, cloudDirty: true }),
            remove: () => draftStore.remove(userId, canvasId),
          }
        : null,
      onStatus: setStatus,
      onDraftUnavailable: () =>
        console.warn("本地草稿不可用（IndexedDB 被禁用或空间不足），已退化为只保存到云端"),
      now: Date.now,
      setTimer: (fn, ms) => window.setTimeout(fn, ms),
      clearTimer: (handle) => window.clearTimeout(handle as number),
    });
  });

  /** 内容有改动：只标脏并排期，真正写草稿和发请求在各自的停手窗口之后 */
  const changed = useCallback(() => coordinator.changed(), [coordinator]);

  /**
   * 立即保存：先写草稿，再等在途请求落地后上传，返回保存后云端是否没有未保存内容。
   * 手动保存、点重试、离开画布前用；失败或有冲突时返回 false。
   */
  const flush = useCallback(
    (options?: { keepalive?: boolean }) => coordinator.flush(options),
    [coordinator],
  );

  /** 用户选了「加载最新」或「另存为」之后：把冲突态清掉，本地未保存的内容不再提交 */
  const dismissConflict = useCallback(() => {
    setConflict(null);
    coordinator.dismissConflict();
  }, [coordinator]);

  /**
   * 改画布名：和图谱保存共用同一个 revision，排在在途保存后面再发，
   * 改完把新 revision 接过来，后面的图谱保存不会撞 409。
   */
  const rename = useCallback(
    async (title: string) => {
      if (coordinator.conflicted) return false;
      try {
        const saved = await coordinator.exclusive(() =>
          updateCanvas(canvasId, { revision: coordinator.version, title }, { silent: true }),
        );
        coordinator.acceptVersion(saved.version);
        return true;
      } catch (error) {
        if (error instanceof ApiError && error.status === 409) {
          try {
            setConflict(await getCanvas(canvasId));
          } catch {
            // 拉不到最新画布就只当改名失败，本地内容还在
          }
        }
        return false;
      } finally {
        coordinator.reschedule();
      }
    },
    [canvasId, coordinator],
  );

  useEffect(() => {
    coordinator.setActive(true);
    const leaving = () => void coordinator.flush({ keepalive: true });
    const onVisibility = () => {
      if (document.visibilityState === "hidden") leaving();
    };
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      // 草稿能兜底时关页不会丢内容，不弹确认；只有没有草稿（或写入失败）时才弹
      if (!coordinator.needsLeaveWarning) return;
      event.preventDefault();
    };
    // 断网后联网：失败状态下立刻再试一次
    const onOnline = () => {
      if (coordinator.hasPendingCloud && !coordinator.conflicted) void coordinator.flush();
    };
    // 退出登录时统一同步所有打开的画布，同步完才清草稿
    const unregisterExitFlush = registerExitFlush(() => coordinator.flush());
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pagehide", leaving);
    window.addEventListener("beforeunload", onBeforeUnload);
    window.addEventListener("online", onOnline);
    return () => {
      // 路由跳转离开画布：同步发起草稿写入，云端上传在后台继续。有冲突时不上传，免得又撞一次
      void coordinator.flush();
      coordinator.setActive(false);
      unregisterExitFlush();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", leaving);
      window.removeEventListener("beforeunload", onBeforeUnload);
      window.removeEventListener("online", onOnline);
    };
  }, [coordinator]);

  /** 当前的服务端版本号（保存和合并都会推进它） */
  const getVersion = useCallback(() => coordinator.version, [coordinator]);
  /** 别处的改动已并进本地：把版本接到它的 revision */
  const mergeVersion = useCallback(
    (version: number) => coordinator.mergeVersion(version),
    [coordinator],
  );
  /** 云端还有没存上的内容（改动没发出去，或请求在途） */
  const hasUnsaved = useCallback(() => coordinator.hasUnsaved, [coordinator]);

  return {
    status,
    conflict,
    changed,
    flush,
    rename,
    dismissConflict,
    getVersion,
    mergeVersion,
    hasUnsaved,
  };
}
