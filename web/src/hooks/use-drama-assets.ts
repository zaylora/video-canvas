import { useCallback, useEffect, useRef, useState } from "react";

import { getCanvas } from "@/api/canvas";
import { assetsFromGraph, type RefAsset } from "@/utils/drama/segment-assets";

export type DramaAssetsState = {
  assets: RefAsset[];
  status: "loading" | "ready" | "error";
  /** 重新读一次画布；返回最新的资产，读取失败（或被更晚的一次读取取代）为 null */
  reload: () => Promise<RefAsset[] | null>;
};

const NONE: RefAsset[] = [];

/**
 * 短剧画布上的资产节点（角色 / 场景 / 道具）：分集卡片计数、片段工作区资产库和 @ 候选都读它。
 * 只读画布，不占编辑锁；角色卡节点的形象、声音就在节点数据里，读到什么就是什么。
 * 页面回到前台时重新读一次，资产页里新生成的形象能跟上；reloadKey 变化也重读（拆解 / 写提示词任务收尾）。
 */
export function useDramaAssets(canvasId: string, reloadKey = 0): DramaAssetsState {
  const [assets, setAssets] = useState<RefAsset[]>(NONE);
  const [status, setStatus] = useState<DramaAssetsState["status"]>("loading");
  const seq = useRef(0);

  const reload = useCallback(async (): Promise<RefAsset[] | null> => {
    const mine = ++seq.current;
    try {
      const canvas = await getCanvas(canvasId);
      if (mine !== seq.current) return null;
      const next = assetsFromGraph(canvas.graph.nodes);
      setAssets(next);
      setStatus("ready");
      return next;
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

  return { assets, status, reload };
}
