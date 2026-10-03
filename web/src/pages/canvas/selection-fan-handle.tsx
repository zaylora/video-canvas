import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useReactFlow, useStore } from "@xyflow/react";
import { Plus } from "lucide-react";
import { motion } from "motion/react";

import { PendingFanLines } from "@/components/canvas";
import { DURATION, EASE_OUT } from "@/lib/motion";
import type { CanvasEdge, CanvasNode, PendingGroup } from "@/types";

/** 把手中心离选框右边缘的距离，屏幕像素（不随缩放变） */
const GAP = 30;
/** 移动超过这个距离才算拖拽，否则当作点击 */
const DRAG_THRESHOLD = 6;

type Point = { x: number; y: number };

/**
 * 多选后选框右侧的「+」：拖出去松手，或直接点一下，都会弹出种类菜单，
 * 选完新建一个节点并引用被选中的所有节点。拖动时每个被选节点各拉一根线汇到指针处。
 * 把手按屏幕像素定大小，画布缩得再小也点得中。
 */
export function SelectionFanHandle({
  ids,
  x,
  y,
  onFanOut,
}: {
  ids: string[];
  /** 选框右边缘（画布坐标） */
  x: number;
  /** 选框垂直中心（画布坐标） */
  y: number;
  onFanOut: (screen: Point, group: PendingGroup) => void;
}) {
  const { getInternalNode, flowToScreenPosition } = useReactFlow<CanvasNode, CanvasEdge>();
  const zoom = useStore((state) => state.transform[2]);
  const [drag, setDrag] = useState<{ froms: Point[]; to: Point } | null>(null);
  const session = useRef<{ froms: Point[]; origin: Point; moved: boolean } | null>(null);

  /** 被选节点右侧出口的屏幕坐标 */
  const exits = () =>
    ids.flatMap((id) => {
      const node = getInternalNode(id);
      const { width, height } = node?.measured ?? {};
      if (!node || !width || !height) return [];
      const { x: nodeX, y: nodeY } = node.internals.positionAbsolute;
      return [flowToScreenPosition({ x: nodeX + width, y: nodeY + height / 2 })];
    });

  const onPointerDown = (event: React.PointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0) return;
    // 别让画布把这次按下当成平移或框选的起点
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    const froms = exits();
    session.current = { froms, origin: { x: event.clientX, y: event.clientY }, moved: false };
  };

  const onPointerMove = (event: React.PointerEvent<HTMLButtonElement>) => {
    const current = session.current;
    if (!current) return;
    const to = { x: event.clientX, y: event.clientY };
    if (!current.moved) {
      current.moved = Math.hypot(to.x - current.origin.x, to.y - current.origin.y) > DRAG_THRESHOLD;
    }
    if (current.moved) setDrag({ froms: current.froms, to });
  };

  const onPointerUp = (event: React.PointerEvent<HTMLButtonElement>) => {
    const current = session.current;
    session.current = null;
    setDrag(null);
    if (!current) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
    const rect = event.currentTarget.getBoundingClientRect();
    // 拖出去就在松手处弹；只是点一下就挨着把手弹
    const screen = current.moved
      ? { x: event.clientX, y: event.clientY }
      : { x: rect.right + 8, y: rect.top + rect.height / 2 };
    onFanOut(screen, { nodeIds: ids, froms: current.froms });
  };

  const cancel = () => {
    session.current = null;
    setDrag(null);
  };

  return (
    <>
      <div
        className="absolute top-0 left-0"
        style={{
          transform: `translate(${x}px, ${y}px) scale(${1 / zoom})`,
          transformOrigin: "0 0",
        }}
      >
        <motion.button
          type="button"
          aria-label="引用选中的节点生成"
          title="拖出去或点击，引用选中的节点生成"
          initial={{ opacity: 0, scale: 0.6 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
          className="bg-canvas text-muted-foreground ring-foreground/30 hover:text-foreground hover:ring-foreground/60 nodrag nopan pointer-events-auto absolute flex size-7 -translate-y-1/2 cursor-crosshair touch-none items-center justify-center rounded-full ring-[1.5px] transition-colors"
          style={{ left: GAP - 14 }}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={cancel}
        >
          <Plus className="size-3.5" />
        </motion.button>
      </div>
      {/* 视口层带着 transform，fixed 会跟着缩放，引导线得挂到 body 上 */}
      {drag && createPortal(<PendingFanLines froms={drag.froms} to={drag.to} />, document.body)}
    </>
  );
}
