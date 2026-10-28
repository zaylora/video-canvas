import { useCallback } from "react";
import { useReactFlow } from "@xyflow/react";

import type { CanvasEdge, CanvasNode } from "@/types";

/** 只选中 id 这一个节点，别的取消选中；没有变化的节点保持原引用 */
export function selectOnly<T extends { id: string; selected?: boolean }>(
  nodes: T[],
  id: string,
): T[] {
  return nodes.map((node) =>
    node.id === id
      ? { ...node, selected: true }
      : node.selected
        ? { ...node, selected: false }
        : node,
  );
}

/** 选中某个节点（不动视口）；节点不在画布上返回 false */
export function useSelectNode() {
  const { setNodes, getNode } = useReactFlow<CanvasNode, CanvasEdge>();
  return useCallback(
    (id: string) => {
      if (!getNode(id)) return false;
      setNodes((nodes) => selectOnly(nodes, id));
      return true;
    },
    [getNode, setNodes],
  );
}

/** 取消所有节点的选中；没有选中的节点保持原引用 */
export function useClearSelection() {
  const { setNodes } = useReactFlow<CanvasNode, CanvasEdge>();
  return useCallback(
    () =>
      setNodes((nodes) =>
        nodes.some((node) => node.selected)
          ? nodes.map((node) => (node.selected ? { ...node, selected: false } : node))
          : nodes,
      ),
    [setNodes],
  );
}

/** 把视口平滑飞到某个节点（不改选中）；节点不在画布上返回 false */
export function useLocateNode() {
  const { fitView, getNode } = useReactFlow<CanvasNode, CanvasEdge>();
  return useCallback(
    (id: string) => {
      if (!getNode(id)) return false;
      void fitView({ nodes: [{ id }], duration: 320, padding: 0.8, maxZoom: 1 });
      return true;
    },
    [fitView, getNode],
  );
}

/** 选中某个节点并把视口平滑飞过去（抽屉、统计栏里点条目时用） */
export function useFocusNode() {
  const { fitView, setNodes, getNode } = useReactFlow<CanvasNode, CanvasEdge>();
  return useCallback(
    (id: string) => {
      if (!getNode(id)) return false;
      setNodes((nodes) => selectOnly(nodes, id));
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
