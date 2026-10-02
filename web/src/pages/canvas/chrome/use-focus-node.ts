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
