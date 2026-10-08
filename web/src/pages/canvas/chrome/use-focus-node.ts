import { useCallback } from "react";
import { useReactFlow } from "@xyflow/react";

import type { CanvasEdge, CanvasNode } from "@/types";

/** 选中某个节点并把视口平滑飞过去（抽屉、统计栏里点条目时用） */
export function useFocusNode() {
  const { fitView, setNodes, getNode } = useReactFlow<CanvasNode, CanvasEdge>();
  return useCallback(
    (id: string) => {
      if (!getNode(id)) return false;
      setNodes((nodes) =>
        nodes.map((node) =>
          node.id === id
            ? { ...node, selected: true }
            : node.selected
              ? { ...node, selected: false }
              : node,
        ),
      );
      void fitView({ nodes: [{ id }], duration: 320, padding: 0.8, maxZoom: 1 });
      return true;
    },
    [fitView, getNode, setNodes],
  );
}

/** 把一批节点一起框进视口（不改选中）；一个都不在画布上时返回 false */
export function useFitNodes() {
  const { fitView, getNode } = useReactFlow<CanvasNode, CanvasEdge>();
  return useCallback(
    (ids: readonly string[]) => {
      const nodes = ids.filter((id) => getNode(id)).map((id) => ({ id }));
      if (nodes.length === 0) return false;
      void fitView({ nodes, duration: 320, padding: 0.3, maxZoom: 1 });
      return true;
    },
    [fitView, getNode],
  );
}
