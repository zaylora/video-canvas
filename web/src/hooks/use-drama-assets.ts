import { useCallback, useEffect, useRef, useState } from "react";

import { getCanvas } from "@/api/canvas";
import { useCharactersStore } from "@/store/characters";
import { assetsFromGraph, type RefAsset } from "@/utils/drama/segment-assets";

export type DramaAssetsState = {
  assets: RefAsset[];
  status: "loading" | "ready" | "error";
  reload: () => Promise<void>;
};

const NONE: RefAsset[] = [];

/**
 * 短剧画布上的资产节点（角色 / 场景 / 道具）：分集卡片计数、片段工作区资产库和 @ 候选都读它。
 * 只读画布，不占编辑锁；角色卡内容顺带读进角色卡缓存（形象、声音实时取自卡）。
 * 页面回到前台时重新读一次，资产页里新生成的形象能跟上；reloadKey 变化也重读（拆解 / 写提示词任务收尾）。
 */
export function useDramaAssets(canvasId: string, reloadKey = 0): DramaAssetsState {
  const [assets, setAssets] = useState<RefAsset[]>(NONE);
  const [status, setStatus] = useState<DramaAssetsState["status"]>("loading");
  const seq = useRef(0);

  const reload = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const canvas = await getCanvas(canvasId);
      if (mine !== seq.current) return;
      const next = assetsFromGraph(canvas.graph.nodes);
      setAssets(next);
      setStatus("ready");
      const ids = next.flatMap((asset) => (asset.characterId ? [asset.characterId] : []));
      if (ids.length > 0) void useCharactersStore.getState().ensure(ids);
    } catch {
      if (mine === seq.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
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
