import { useEffect, useState } from "react";
import { useNodeId, useStore, useStoreApi } from "@xyflow/react";

import { nextLowDetail } from "@/utils/canvas/media-lod";

/**
 * 画布当前是否处于低清档（缩放低，该用缩略图）。
 * 订阅缩放变化，但只在带滞回的布尔值翻转时才更新状态：
 * 返回原始 zoom 会让每次滚轮都重渲染所有节点。
 * @returns true 表示该用缩略图
 */
export function useLowDetail(): boolean {
  const store = useStoreApi();
  const [low, setLow] = useState(() => nextLowDetail(false, store.getState().transform[2]));

  useEffect(() => {
    return store.subscribe((state) => {
      setLow((prev) => nextLowDetail(prev, state.transform[2]));
    });
  }, [store]);

  return low;
}

/**
 * 当前节点是否被选中；在节点之外使用时恒为 false。
 * @returns 选中状态
 */
export function useNodeSelected(): boolean {
  const id = useNodeId();
  return useStore((state) => (id ? (state.nodeLookup.get(id)?.selected ?? false) : false));
}
