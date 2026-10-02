import { useCallback, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { motion } from "motion/react";
import { NodeToolbar, Position, useReactFlow } from "@xyflow/react";

import { NodeHistoryStrip } from "@/components/canvas/node-history-strip";
import {
  PANEL_OFFSET,
  useCanvasDragging,
  usePanelPlacement,
} from "@/components/canvas/hooks/use-panel-placement";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { DURATION, EASE_OUT } from "@/lib/motion";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { activeOutputIdOf, readOutputs, selectOutput } from "@/utils/canvas/outputs";

/** 历史浮条离节点卡片顶边的距离：让过卡片上方的标题行 */
const STRIP_OFFSET_TOP = 44;
const STRIP_OFFSET_BOTTOM = 14;

/**
 * 选中节点时浮出的两块：上方的「节点生成历史」浮条、下方的生成面板。
 * 只在选中时挂载，所以摆法计算（订阅视口变化）只花在一个节点上。
 * 面板下方放不下时翻到节点上方，浮条随之挪到节点下方；拖动画布或节点时两者淡下去。
 */
export function NodeOverlays({
  id,
  data,
  showHistory,
  children,
}: {
  id: string;
  data: CanvasNodeData;
  /** 文本节点没有素材版本，不显示历史浮条 */
  showHistory: boolean;
  /** 生成面板，拿到算好的宽度 */
  children: (width: number) => ReactNode;
}) {
  const { updateNodeData } = useReactFlow<CanvasNode, CanvasEdge>();
  const history = useCanvasHistoryContext();
  const panelRef = useRef<HTMLDivElement>(null);
  const [panelHeight, setPanelHeight] = useState(300);
  const { position, width, shift } = usePanelPlacement(id, panelHeight);
  const dragging = useCanvasDragging(id);
  const flipped = position === Position.Top;

  useLayoutEffect(() => {
    const element = panelRef.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) =>
      setPanelHeight(Math.round(entry.borderBoxSize[0]?.blockSize ?? entry.contentRect.height)),
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const selectVersion = useCallback(
    (outputId: string) => {
      // 生成中不切：正文显示的是任务进度，切了也看不到，还会把状态改乱
      // 失败后点当前那一版，等于把它找回来，照样放行
      if (data.status === "running") return;
      if (outputId === activeOutputIdOf(data) && data.status === "done") return;
      history?.record();
      updateNodeData(id, (node) => {
        const patch = selectOutput(node.data, outputId);
        return patch ? { ...patch, status: "done", error: null } : {};
      });
    },
    [data, history, id, updateNodeData],
  );

  const fade = {
    opacity: dragging ? 0.35 : 1,
    pointerEvents: dragging ? ("none" as const) : undefined,
  };

  return (
    <>
      {showHistory && (
        <NodeToolbar
          isVisible
          position={flipped ? Position.Bottom : Position.Top}
          offset={flipped ? STRIP_OFFSET_BOTTOM : STRIP_OFFSET_TOP}
        >
          <div style={{ ...fade, transition: "opacity 150ms" }}>
            <NodeHistoryStrip
              nodeId={id}
              outputs={readOutputs(data)}
              activeId={activeOutputIdOf(data)}
              running={data.status === "running"}
              onSelect={selectVersion}
            />
          </div>
        </NodeToolbar>
      )}
      <NodeToolbar isVisible position={position} offset={PANEL_OFFSET}>
        <motion.div
          ref={panelRef}
          initial={{ opacity: 0, y: flipped ? 6 : -6, scale: 0.985 }}
          animate={{ opacity: fade.opacity, y: 0, scale: 1 }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          style={{ translate: `${shift}px 0`, pointerEvents: fade.pointerEvents }}
        >
          {children(width)}
        </motion.div>
      </NodeToolbar>
    </>
  );
}
