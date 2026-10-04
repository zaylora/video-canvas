import { useCallback, useMemo } from "react";

import type { CanvasNode, FlowNode } from "@/types";
import { isGroupNode, mergeContentNodes } from "@/utils/canvas/group";

/**
 * 画布节点表里既有素材节点也有组。任务回填、建节点、上传这些代码只认素材节点，
 * 用这个适配器把完整节点表收窄成素材节点，改完再把组并回去，它们不用认识组。
 */
export function useContentNodes(
  nodes: FlowNode[],
  setNodes: React.Dispatch<React.SetStateAction<FlowNode[]>>,
) {
  const contentNodes = useMemo(
    () => nodes.filter((node): node is CanvasNode => !isGroupNode(node)),
    [nodes],
  );
  const setContentNodes = useCallback<React.Dispatch<React.SetStateAction<CanvasNode[]>>>(
    (action) =>
      setNodes((previous) => {
        const content = previous.filter((node): node is CanvasNode => !isGroupNode(node));
        const next = typeof action === "function" ? action(content) : action;
        return next === content ? previous : mergeContentNodes(previous, next);
      }),
    [setNodes],
  );
  return [contentNodes, setContentNodes] as const;
}
