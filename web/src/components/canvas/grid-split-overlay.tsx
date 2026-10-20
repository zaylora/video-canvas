import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import { motion } from "motion/react";
import {
  NodeToolbar,
  Position,
  ViewportPortal,
  useStore,
  type ReactFlowState,
} from "@xyflow/react";
import { RotateCcw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useGridSplitNodes } from "@/hooks/use-grid-split-nodes";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useGridSplitStore } from "@/store/grid-split";
import type { CanvasNodeData } from "@/types";
import { clampLine, evenLines, gridSplitLayerStyle, isValidGrid } from "@/utils/canvas/grid-split";

/** 切分线的拖动命中区宽度（屏幕像素）：线本身只有 1.5px，命中区要好点 */
const HIT_SIZE = 14;

/** 线本身的粗细（屏幕像素） */
const LINE_SIZE = 1.5;

/** 角标、按钮离图片边的距离（屏幕像素） */
const CHIP_INSET = 8;

/** 方向键微调线的步长（占边长的比例），按住 Shift 用大步长 */
const KEY_STEP = 0.01;
const KEY_STEP_BIG = 0.05;

/** 功能栏离图片下边的距离（屏幕像素） */
const BAR_OFFSET = 12;

/** 切分线的方向：x 是竖线（左右拖），y 是横线（上下拖） */
type Axis = "x" | "y";

/** 方向键对应的移动方向，只认和线垂直的那一组 */
const ARROW_DIRECTION: Record<Axis, Record<string, 1 | -1>> = {
  x: { ArrowLeft: -1, ArrowRight: 1 },
  y: { ArrowUp: -1, ArrowDown: 1 },
};

/** 两组线是否和均分一模一样（决定「重置均分」是否可点） */
const isEven = (lines: readonly number[], n: number) => {
  const even = evenLines(n);
  return (
    lines.length === even.length && lines.every((value, i) => Math.abs(value - even[i]) < 1e-9)
  );
};

/** 图片节点此刻在画布上的位置和大小（画布坐标）；还没量到时为 null */
type NodeBox = { x: number; y: number; w: number; h: number };

/**
 * 切分线图层：画在画布坐标系里，随缩放一起变；命中区、线宽、按钮用反向缩放保持固定的屏幕大小。
 * 线的位置是占边长的 0 到 1 小数，拖动时按画布坐标的位移换算，和图片真实像素、节点显示大小都无关。
 * @param box 图片节点的位置和大小
 * @param zoom 画布缩放
 * @param xs 竖线位置
 * @param ys 横线位置
 * @param rows 行数，只用于角标
 * @param cols 列数，只用于角标
 * @param onMove 某条线被拖到了新位置（已经过 clampLine）
 * @param onReset 点了「重置均分」
 */
function SplitLines({
  box,
  zoom,
  xs,
  ys,
  rows,
  cols,
  onMove,
  onReset,
}: {
  box: NodeBox;
  zoom: number;
  xs: number[];
  ys: number[];
  rows: number;
  cols: number;
  onMove: (axis: Axis, index: number, value: number) => void;
  onReset: () => void;
}) {
  const drag = useRef<{
    axis: Axis;
    index: number;
    startClient: number;
    startValue: number;
  } | null>(null);

  const begin =
    (axis: Axis, index: number, value: number) => (event: PointerEvent<HTMLElement>) => {
      // 别让画布把这次按下当成拖画布 / 框选 / 点空白
      event.stopPropagation();
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = {
        axis,
        index,
        startClient: axis === "x" ? event.clientX : event.clientY,
        startValue: value,
      };
    };
  const move = (event: PointerEvent<HTMLElement>) => {
    const current = drag.current;
    if (!current) return;
    const client = current.axis === "x" ? event.clientX : event.clientY;
    const length = current.axis === "x" ? box.w : box.h;
    onMove(
      current.axis,
      current.index,
      current.startValue + (client - current.startClient) / zoom / length,
    );
  };
  const end = () => {
    drag.current = null;
  };
  const key = (axis: Axis, index: number, value: number) => (event: KeyboardEvent<HTMLElement>) => {
    const direction = ARROW_DIRECTION[axis][event.key];
    if (!direction) return;
    event.preventDefault();
    event.stopPropagation();
    onMove(axis, index, value + direction * (event.shiftKey ? KEY_STEP_BIG : KEY_STEP));
  };

  const hit = HIT_SIZE / zoom;
  const line = LINE_SIZE / zoom;
  const outline = `0 0 0 ${1 / zoom}px oklch(0 0 0 / 0.45)`;
  const chipBase =
    "pointer-events-auto absolute flex h-7 items-center gap-1.5 rounded-lg bg-black/65 px-2.5 text-xs whitespace-nowrap text-white ring-1 ring-white/20 backdrop-blur-sm";

  const lines = [
    ...xs.map((value, index) => ({ axis: "x" as const, index, value })),
    ...ys.map((value, index) => ({ axis: "y" as const, index, value })),
  ];

  return (
    <ViewportPortal>
      <div
        className="nodrag nopan nowheel pointer-events-none absolute"
        style={gridSplitLayerStyle(box)}
      >
        {lines.map(({ axis, index, value }) => {
          const vertical = axis === "x";
          const style: CSSProperties = vertical
            ? { left: value * box.w, top: 0, height: box.h, width: hit, translate: "-50% 0" }
            : { top: value * box.h, left: 0, width: box.w, height: hit, translate: "0 -50%" };
          return (
            <div
              key={`${axis}${index}`}
              role="slider"
              tabIndex={0}
              aria-label={vertical ? `第 ${index + 1} 条竖线` : `第 ${index + 1} 条横线`}
              aria-orientation={vertical ? "horizontal" : "vertical"}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(value * 100)}
              aria-valuetext={`${(value * 100).toFixed(1)}%`}
              className="group/line pointer-events-auto absolute touch-none outline-none"
              style={{ ...style, cursor: vertical ? "col-resize" : "row-resize" }}
              onPointerDown={begin(axis, index, value)}
              onPointerMove={move}
              onPointerUp={end}
              onPointerCancel={end}
              onKeyDown={key(axis, index, value)}
            >
              <span
                className="pointer-events-none absolute bg-white transition-colors duration-100 group-hover/line:bg-[oklch(0.78_0.12_265)] group-focus-visible/line:bg-[oklch(0.78_0.12_265)]"
                style={
                  vertical
                    ? {
                        left: "50%",
                        top: 0,
                        bottom: 0,
                        width: line,
                        translate: "-50% 0",
                        boxShadow: outline,
                      }
                    : {
                        top: "50%",
                        left: 0,
                        right: 0,
                        height: line,
                        translate: "0 -50%",
                        boxShadow: outline,
                      }
                }
              />
            </div>
          );
        })}

        <button
          type="button"
          disabled={isEven(xs, cols) && isEven(ys, rows)}
          className={cn(
            chipBase,
            "hover:bg-black/80 disabled:pointer-events-none disabled:opacity-40 focus-visible:ring-2 focus-visible:ring-white/70 outline-none",
          )}
          style={{
            left: CHIP_INSET / zoom,
            top: CHIP_INSET / zoom,
            scale: 1 / zoom,
            transformOrigin: "top left",
          }}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={onReset}
        >
          <RotateCcw className="size-3.5" />
          重置均分
        </button>
        <span
          className={cn(chipBase, "pointer-events-none tabular-nums")}
          style={{
            right: CHIP_INSET / zoom,
            top: CHIP_INSET / zoom,
            scale: 1 / zoom,
            transformOrigin: "top right",
          }}
        >
          {rows}×{cols} · {rows * cols} 格
        </span>
      </div>
    </ViewportPortal>
  );
}

/**
 * 宫格切分编辑态的内容：线的位置只活在这里（0 到 1 小数），进入时均分，退出即丢弃。
 * 外层用行列做 key，换宫格时自然重来。
 */
function GridSplitEditor({ id, data }: { id: string; data: CanvasNodeData }) {
  const rows = useGridSplitStore((state) => state.rows);
  const cols = useGridSplitStore((state) => state.cols);
  const leave = useGridSplitStore((state) => state.leave);
  const { split } = useGridSplitNodes();
  const zoom = useStore((state: ReactFlowState) => state.transform[2]);

  // 图片节点左上角的画布坐标和大小，拼成字符串让选择器只在真的变了时才触发重渲染
  const boxKey = useStore(
    useCallback(
      (state: ReactFlowState) => {
        const node = state.nodeLookup.get(id);
        const w = node?.measured.width;
        const h = node?.measured.height;
        return node && w && h
          ? `${node.internals.positionAbsolute.x},${node.internals.positionAbsolute.y},${w},${h}`
          : "";
      },
      [id],
    ),
  );

  const [xs, setXs] = useState(() => evenLines(cols));
  const [ys, setYs] = useState(() => evenLines(rows));

  // Esc 退出
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") leave(id);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [id, leave]);

  if (!boxKey) return null;
  const [x, y, w, h] = boxKey.split(",").map(Number);
  const box: NodeBox = { x, y, w, h };
  const count = rows * cols;
  const ready = !!data.src && isValidGrid(rows, cols);

  const onMove = (axis: Axis, index: number, value: number) => {
    const update = (lines: number[]) => {
      const next = [...lines];
      next[index] = clampLine(lines, index, value);
      return next;
    };
    if (axis === "x") setXs(update);
    else setYs(update);
  };
  const onReset = () => {
    setXs(evenLines(cols));
    setYs(evenLines(rows));
  };
  const confirm = () => {
    if (!ready) return;
    leave(id);
    void split(id, rows, cols, xs, ys);
  };

  return (
    <>
      <SplitLines
        box={box}
        zoom={zoom}
        xs={xs}
        ys={ys}
        rows={rows}
        cols={cols}
        onMove={onMove}
        onReset={onReset}
      />
      <NodeToolbar isVisible position={Position.Bottom} offset={BAR_OFFSET}>
        <motion.div
          className="nodrag nopan flex gap-2"
          initial={{ opacity: 0, y: -6 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
        >
          <Button variant="outline" size="sm" onClick={() => leave(id)}>
            取消
          </Button>
          <Button size="sm" disabled={!ready} onClick={confirm}>
            确认切分 · {count} 格
          </Button>
        </motion.div>
      </NodeToolbar>
    </>
  );
}

/**
 * 宫格切分模式：图片上画出可拖的切分线，确认后每一格建一个新图片节点（设计稿 §4）。
 * 取代这个节点平时的功能区和生成面板；确认前不建任何节点，取消、取消选中、多选都直接丢弃切分线。
 * @param id 被切分的图片节点 id
 * @param data 该节点的数据
 */
export function GridSplitOverlay({ id, data }: { id: string; data: CanvasNodeData }) {
  const rows = useGridSplitStore((state) => state.rows);
  const cols = useGridSplitStore((state) => state.cols);
  return <GridSplitEditor key={`${rows}x${cols}`} id={id} data={data} />;
}
