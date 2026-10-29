import { useCallback, useRef } from "react";
import { useReactFlow, type OnNodeDrag } from "@xyflow/react";

import type { CanvasEdge, FlowNode } from "@/types";
import { isGroupNode, resolveParent, setParent } from "@/utils/canvas/group";

import type { GroupUi } from "./group-ui";

const nodeElement = (id: string) =>
  document.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(id)}"]`);

/**
 * 节点拖动与组的关系（设计稿 6.10）：
 * - 只有被**亲手拖动**的节点才重新判定归属：松手时中心点在某个组框里就入组，出框就退组；
 *   整组被拖、缩放组框、打组时压着的别的节点都不会入组；
 * - 拖动途中，会入组的目标组描边提亮（直接改 DOM 属性，不进 React state，免得整张画布每帧重渲染）；
 * - 拖的是组本身时，通知界面状态收起工具条。
 */
export function useGroupDrag(ui: Pick<GroupUi, "setDraggingId">) {
  const { getNodes, setNodes } = useReactFlow<FlowNode, CanvasEdge>();
  const { setDraggingId } = ui;
  const lit = useRef<ReadonlySet<string>>(new Set());

  const highlight = useCallback((next: ReadonlySet<string>) => {
    for (const id of lit.current)
      if (!next.has(id)) nodeElement(id)?.removeAttribute("data-drop-target");
    for (const id of next)
      if (!lit.current.has(id)) nodeElement(id)?.setAttribute("data-drop-target", "");
    lit.current = next;
  }, []);

  /** 被拖节点各自会落进哪个组；拖动中的位置取 xyflow 传来的最新值 */
  const dropTargets = useCallback(
    (dragged: FlowNode[]) => {
      const all = getNodes();
      if (!all.some(isGroupNode)) return new Set<string>();
      const live = new Map(dragged.map((node) => [node.id, node]));
      const merged = all.map((node) => live.get(node.id) ?? node);
      const skip = new Set(dragged.filter(isGroupNode).map((node) => node.id));
      const out = new Set<string>();
      for (const node of dragged) {
        if (isGroupNode(node)) continue;
        const parent = resolveParent(node, merged, skip);
        if (parent) out.add(parent);
      }
      return out;
    },
    [getNodes],
  );

  const onNodeDragStart = useCallback<OnNodeDrag<FlowNode>>(
    (_, node) => {
      if (isGroupNode(node)) setDraggingId(node.id);
    },
    [setDraggingId],
  );

  const onNodeDrag = useCallback<OnNodeDrag<FlowNode>>(
    (_, __, dragged) => highlight(dropTargets(dragged)),
    [dropTargets, highlight],
  );

  const onNodeDragStop = useCallback<OnNodeDrag<FlowNode>>(
    (_, __, dragged) => {
      highlight(new Set());
      setDraggingId(null);
      const skip = new Set(dragged.filter(isGroupNode).map((node) => node.id));
      setNodes((nodes) => {
        let next = nodes;
        for (const node of dragged) {
          if (isGroupNode(node)) continue;
          const current = next.find((candidate) => candidate.id === node.id);
          if (current) next = setParent(next, node.id, resolveParent(current, next, skip));
        }
        return next;
      });
    },
    [highlight, setDraggingId, setNodes],
  );

  return { onNodeDragStart, onNodeDrag, onNodeDragStop };
}
