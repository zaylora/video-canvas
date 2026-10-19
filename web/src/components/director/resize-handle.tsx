import { useRef, useState, type CSSProperties } from "react";

import { cn } from "@/lib/utils";

/** 键盘每按一下方向键挪多少像素 */
export const NUDGE_STEP = 16;

/**
 * 面板之间的分界线：按住拖动调节两边的大小，双击还原默认，聚焦后用方向键微调。
 * 它只负责手势：按下时问父组件要「当前尺寸」，拖动时告诉父组件「鼠标挪了多少像素」，
 * 怎么换算成面板尺寸、夹在什么范围，都由父组件决定（所以左右两侧的面板、按比例分栏都能用）。
 * 位置由父组件用 style 摆（绝对定位，压在两个面板的交界上）。
 * @param props.orientation vertical：竖直的分界线，左右拖；horizontal：水平的分界线，上下拖
 * @param props.label 读屏播报的名字，比如「资产面板宽度」
 * @param props.onStart 按下时调用，返回此刻被调节的那个尺寸（拖动期间都以它为起点）
 * @param props.onMove 拖动中调用：起点尺寸，和鼠标相对按下位置挪的像素（右 / 下为正）
 * @param props.onEnd 一次调节结束（松手、键盘按完一下）：记住尺寸
 * @param props.onNudge 方向键：-1 向左 / 上，1 向右 / 下
 */
export function ResizeHandle({
  orientation,
  label,
  style,
  onStart,
  onMove,
  onEnd,
  onReset,
  onNudge,
}: {
  orientation: "vertical" | "horizontal";
  label: string;
  style: CSSProperties;
  onStart: () => number;
  onMove: (start: number, delta: number) => void;
  onEnd: () => void;
  onReset: () => void;
  onNudge: (direction: -1 | 1) => void;
}) {
  const vertical = orientation === "vertical";
  const [dragging, setDragging] = useState(false);
  const drag = useRef<{ origin: number; start: number } | null>(null);
  const position = (event: { clientX: number; clientY: number }) =>
    vertical ? event.clientX : event.clientY;

  return (
    <>
      <div
        role="separator"
        aria-orientation={vertical ? "vertical" : "horizontal"}
        aria-label={label}
        title={`${label}：拖动调节，双击还原`}
        tabIndex={0}
        style={style}
        onPointerDown={(event) => {
          if (event.button !== 0) return;
          event.currentTarget.setPointerCapture(event.pointerId);
          drag.current = { origin: position(event), start: onStart() };
          setDragging(true);
        }}
        onPointerMove={(event) => {
          if (drag.current) onMove(drag.current.start, position(event) - drag.current.origin);
        }}
        onPointerUp={(event) => {
          if (!drag.current) return;
          drag.current = null;
          setDragging(false);
          if (event.currentTarget.hasPointerCapture(event.pointerId)) {
            event.currentTarget.releasePointerCapture(event.pointerId);
          }
          onEnd();
        }}
        onPointerCancel={() => {
          drag.current = null;
          setDragging(false);
          onEnd();
        }}
        onDoubleClick={onReset}
        onKeyDown={(event) => {
          const back = vertical ? "ArrowLeft" : "ArrowUp";
          const forward = vertical ? "ArrowRight" : "ArrowDown";
          if (event.key !== back && event.key !== forward) return;
          event.preventDefault();
          event.stopPropagation();
          onNudge(event.key === back ? -1 : 1);
          onEnd();
        }}
        className={cn(
          "group absolute z-20 touch-none outline-none",
          vertical ? "cursor-col-resize" : "cursor-row-resize",
        )}
      >
        {/* 看得见的细线：平时不显示，悬停、聚焦、拖动时亮起 */}
        <span
          className={cn(
            "bg-ring/0 group-hover:bg-ring/70 group-focus-visible:bg-ring pointer-events-none absolute transition-colors",
            vertical
              ? "inset-y-0 left-1/2 w-0.5 -translate-x-1/2"
              : "inset-x-0 top-1/2 h-0.5 -translate-y-1/2",
            dragging && "bg-ring",
          )}
        />
      </div>
      {/* 拖动时盖一层透明的全屏层：光标一直是调节样式，也不会误触下面的 3D 视口 */}
      {dragging && (
        <div
          className={cn("fixed inset-0 z-50", vertical ? "cursor-col-resize" : "cursor-row-resize")}
        />
      )}
    </>
  );
}
