import { createContext, useCallback, useContext, useRef, useState } from "react";
import type { NodeChange, OnNodeDrag } from "@xyflow/react";

import type { FlowNode } from "@/types";

/**
 * 节点浮层（历史浮条、生成面板）的开关：拖动顺带选中的节点不打开，点一下才打开。
 * xyflow 的节点拖动只要动过就吞掉随后的 click（nodeClickDistance = 0），
 * 所以「按下时还没选中、随后被拖」就是拖动顺带选中，这些节点记进 dragSelected；
 * 点击节点或节点取消选中时把它拿掉。新建、定位这类程序选中不经过拖动，照常打开。
 */
export function useOverlayGate(getNodes: () => FlowNode[]) {
  const [dragSelected, setDragSelected] = useState<ReadonlySet<string>>(() => new Set());
  const selectedAtPointerDown = useRef<ReadonlySet<string>>(new Set());

  const remove = useCallback((ids: string[]) => {
    setDragSelected((current) => {
      if (!ids.some((id) => current.has(id))) return current;
      const next = new Set(current);
      for (const id of ids) next.delete(id);
      return next;
    });
  }, []);

  /** 挂在画布根节点的 onPointerDownCapture：赶在 xyflow 处理之前记下谁已经是选中的 */
  const onPointerDownCapture = useCallback(() => {
    selectedAtPointerDown.current = new Set(
      getNodes()
        .filter((node) => node.selected)
        .map((node) => node.id),
    );
  }, [getNodes]);

  const onNodeDragStart = useCallback<OnNodeDrag<FlowNode>>((_, __, dragged) => {
    const fresh = dragged
      .map((node) => node.id)
      .filter((id) => !selectedAtPointerDown.current.has(id));
    if (fresh.length === 0) return;
    setDragSelected((current) => new Set([...current, ...fresh]));
  }, []);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: FlowNode) => remove([node.id]),
    [remove],
  );

  /** 放进 onNodesChange 里：取消选中、被删掉的节点不再记着 */
  const pruneOnChange = useCallback(
    (changes: NodeChange<FlowNode>[]) => {
      const gone = changes.flatMap((change) =>
        (change.type === "select" && !change.selected) || change.type === "remove"
          ? [change.id]
          : [],
      );
      if (gone.length) remove(gone);
    },
    [remove],
  );

  return { dragSelected, onPointerDownCapture, onNodeDragStart, onNodeClick, pruneOnChange };
}

const OverlayGateContext = createContext<ReadonlySet<string>>(new Set());
export const OverlayGateProvider = OverlayGateContext.Provider;

/** 这个节点是不是拖动顺带选中的（是的话不打开浮层） */
export const useDragSelected = (id: string) => useContext(OverlayGateContext).has(id);
