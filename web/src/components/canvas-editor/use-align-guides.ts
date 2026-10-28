import { useCallback, useEffect, useRef } from "react";
import { useReactFlow, type NodeChange, type OnNodeDrag } from "@xyflow/react";

import { useAlignGuidesStore } from "@/store/align-guides";
import type { CanvasEdge, FlowNode } from "@/types";
import { adjustForAlign, startAlignSession, type AlignSession } from "@/utils/canvas/align-session";

/**
 * 拖动节点时的智能对齐与磁吸（设计稿 docs/design/画布对齐线）。常开，没有开关、没有临时关闭键。
 * 拖动开始建会话（只有单个普通节点才建），拖动中 adjustChanges 改写被拖节点的位置变更，
 * 辅助线写进独立 store，只有覆盖层订阅，所以拖动时 FlowCanvas 不会因为它重渲染。
 * 三个回调的引用都是稳定的。把 adjustChanges 放在 onNodesChange 里、applyNodesChange 之前调用。
 */
export function useAlignGuides() {
  const { getNodes, getViewport } = useReactFlow<FlowNode, CanvasEdge>();
  const sessionRef = useRef<AlignSession | null>(null);

  useEffect(() => () => useAlignGuidesStore.getState().clear(), []);

  const onNodeDragStart = useCallback<OnNodeDrag<FlowNode>>(
    (_, __, dragged) => {
      sessionRef.current = startAlignSession(getNodes(), dragged);
    },
    [getNodes],
  );

  const onNodeDragStop = useCallback(() => {
    sessionRef.current = null;
    useAlignGuidesStore.getState().clear();
  }, []);

  const adjustChanges = useCallback(
    (changes: NodeChange<FlowNode>[]) => {
      const session = sessionRef.current;
      if (!session) return changes;
      const out = adjustForAlign(session, changes, getViewport().zoom);
      if (out.guides) useAlignGuidesStore.getState().setGuides(out.guides);
      return out.changes;
    },
    [getViewport],
  );

  return { onNodeDragStart, onNodeDragStop, adjustChanges };
}
