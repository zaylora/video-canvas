import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { getCanvas } from "@/api/canvas";
import { useTasksStore } from "@/store/tasks";
import type { TaskMap } from "@/utils/tasks/merge";
import { assetsFromGraph, type RefAsset } from "@/utils/drama/segment-assets";
import { reconcileTaskIds } from "@/utils/ws/task-events";

export type DramaAssetsState = {
  assets: RefAsset[];
  status: "loading" | "ready" | "error";
  /** 重新读一次画布；返回最新的资产，读取失败（或被更晚的一次读取取代）为 null */
  reload: () => Promise<RefAsset[] | null>;
};

const NONE: RefAsset[] = [];

/** 声音任务状态随任务快照更新，失败任务不妨碍继续使用先前已绑定的音色。 */
const withVoiceTasks = (assets: RefAsset[], tasks: TaskMap): RefAsset[] =>
  assets.map((asset) =>
    asset.voiceTaskId ? { ...asset, voiceTaskStatus: tasks[asset.voiceTaskId]?.status } : asset,
  );

/**
 * 短剧画布上的资产节点（角色 / 场景 / 道具）：分集卡片计数、片段工作区资产库和 @ 候选都读它。
 * 只读画布，不占编辑锁；角色卡节点的形象、声音就在节点数据里，读到什么就是什么。
 * 页面回到前台时重新读一次，资产页里新生成的形象能跟上；reloadKey 变化也重读（拆解 / 写提示词任务收尾）。
 */
export function useDramaAssets(canvasId: string, reloadKey = 0): DramaAssetsState {
  const [assets, setAssets] = useState<RefAsset[]>(NONE);
  const [status, setStatus] = useState<DramaAssetsState["status"]>("loading");
  const seq = useRef(0);
  const tasks = useTasksStore((state) => state.tasks);
  const current = useMemo(() => withVoiceTasks(assets, tasks), [assets, tasks]);

  const reload = useCallback(async (): Promise<RefAsset[] | null> => {
    const mine = ++seq.current;
    try {
      const canvas = await getCanvas(canvasId);
      if (mine !== seq.current) return null;
      const next = assetsFromGraph(canvas.graph.nodes);
      const voiceIds = [
        ...new Set(next.flatMap((asset) => (asset.voiceTaskId ? [asset.voiceTaskId] : []))),
      ];
      if (voiceIds.length) await reconcileTaskIds(voiceIds);
      if (mine !== seq.current) return null;
      setAssets(next);
      setStatus("ready");
      return withVoiceTasks(next, useTasksStore.getState().tasks);
    } catch {
      if (mine === seq.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
      return null;
    }
  }, [canvasId]);

  useEffect(() => {
    void reload();
  }, [reload, reloadKey]);

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") void reload();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [reload]);

  return { assets: current, status, reload };
}
