import {
  useCallback,
  useRef,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import { ViewportPortal, useStore, type ReactFlowState } from "@xyflow/react";
import { toast } from "sonner";

import { cn } from "@/lib/utils";
import {
  frameHeight,
  frameWidth,
  moveFrame,
  resizeFrame,
  type FrameHandle,
  type OriginSize,
  type OutpaintFrame,
} from "@/utils/canvas/outpaint-frame";

/** 键盘微调的步长（画布坐标），按住 Shift 用大步长 */
const KEY_STEP = 8;
const KEY_STEP_BIG = 32;

/** 到 3 倍上限时的提示，固定 id，连续拖动也只会有一条 */
const LIMIT_TOAST_ID = "outpaint-limit";

/** 棋盘格 = 透明：和原图拼成一张图后交给模型，格子大小固定 24 屏幕像素 */
const checkerStyle = (zoom: number): CSSProperties => {
  const a = "color-mix(in oklch, var(--foreground) 5%, var(--canvas-bg))";
  const b = "color-mix(in oklch, var(--foreground) 10%, var(--canvas-bg))";
  return {
    backgroundColor: a,
    backgroundImage: `conic-gradient(${b} 25%, transparent 0 50%, ${b} 0 75%, transparent 0)`,
    backgroundSize: `${24 / zoom}px ${24 / zoom}px`,
  };
};

const ARROW_DELTA: Record<string, [number, number]> = {
  ArrowLeft: [-1, 0],
  ArrowRight: [1, 0],
  ArrowUp: [0, -1],
  ArrowDown: [0, 1],
};

/** 每个手柄的位置（相对框左上角的比例）、光标、读屏名字 */
const HANDLES: Array<{
  handle: FrameHandle;
  at: [number, number];
  cursor: string;
  label: string;
}> = [
  { handle: "t", at: [0.5, 0], cursor: "ns-resize", label: "上边" },
  { handle: "b", at: [0.5, 1], cursor: "ns-resize", label: "下边" },
  { handle: "l", at: [0, 0.5], cursor: "ew-resize", label: "左边" },
  { handle: "r", at: [1, 0.5], cursor: "ew-resize", label: "右边" },
  { handle: "tl", at: [0, 0], cursor: "nwse-resize", label: "左上角（等比）" },
  { handle: "tr", at: [1, 0], cursor: "nesw-resize", label: "右上角（等比）" },
  { handle: "bl", at: [0, 1], cursor: "nesw-resize", label: "左下角（等比）" },
  { handle: "br", at: [1, 1], cursor: "nwse-resize", label: "右下角（等比）" },
];

/** 手柄的可见部分：边上是一根短粗条，角上是 L 形折角 */
function HandleMark({ handle }: { handle: FrameHandle }) {
  const base = "pointer-events-none absolute shadow-[0_0_0_1px_oklch(0_0_0/0.45)]";
  if (handle === "t" || handle === "b")
    return <span className={cn(base, "h-1 w-6 rounded-sm bg-white")} />;
  if (handle === "l" || handle === "r")
    return <span className={cn(base, "h-6 w-1 rounded-sm bg-white")} />;
  const corner = {
    // 折角的外角离框角 5px，向框里伸 9px（和原型一致）
    tl: "top-[13px] left-[13px] border-t-[3px] border-l-[3px]",
    tr: "top-[13px] right-[13px] border-t-[3px] border-r-[3px]",
    bl: "bottom-[13px] left-[13px] border-b-[3px] border-l-[3px]",
    br: "right-[13px] bottom-[13px] border-r-[3px] border-b-[3px]",
  }[handle];
  return (
    <span
      className={cn(
        "pointer-events-none absolute size-3.5 border-white drop-shadow-[0_0_1px_oklch(0_0_0/0.55)]",
        corner,
      )}
    />
  );
}

/**
 * 扩图框（设计稿 6.17）：画在画布坐标系里，随缩放一起变；棋盘格是要新生成的透明区域，
 * 原图所在的位置留空，所以原图本身不用重画。手柄和尺寸角标用反向缩放保持固定的屏幕大小。
 * 框内按住拖动是平移（框始终包住原图），8 个手柄缩放；方向键微调，Shift 加大步长。
 * @param nodeId 原图节点 id，框的位置跟着它
 * @param origin 原图节点的大小（画布坐标）
 * @param frame 当前框，相对原图左上角
 * @param onChange 框变了
 * @param sizeLabel 尺寸角标的文字；不知道时不显示
 */
export function OutpaintFrameLayer({
  nodeId,
  origin,
  frame,
  onChange,
  sizeLabel,
  disabled,
}: {
  nodeId: string;
  origin: OriginSize;
  frame: OutpaintFrame;
  onChange: (frame: OutpaintFrame) => void;
  sizeLabel?: string;
  disabled?: boolean;
}) {
  // 原图左上角的画布坐标，拼成字符串让选择器只在真的变了时才触发重渲染
  const position = useStore(
    useCallback(
      (state: ReactFlowState) => {
        const node = state.nodeLookup.get(nodeId);
        return node
          ? `${node.internals.positionAbsolute.x},${node.internals.positionAbsolute.y}`
          : "";
      },
      [nodeId],
    ),
  );
  const zoom = useStore((state: ReactFlowState) => state.transform[2]);
  const drag = useRef<{
    handle: FrameHandle | "move";
    startX: number;
    startY: number;
    start: OutpaintFrame;
  } | null>(null);

  if (!position) return null;
  const [px, py] = position.split(",").map(Number);
  const fw = frameWidth(frame);
  const fh = frameHeight(frame);

  const apply = (handle: FrameHandle | "move", start: OutpaintFrame, dx: number, dy: number) => {
    if (handle === "move") {
      onChange(moveFrame(origin, start, dx, dy));
      return;
    }
    const result = resizeFrame(handle, origin, start, dx, dy);
    onChange(result.frame);
    if (result.limited) toast("已到 3 倍上限", { id: LIMIT_TOAST_ID, duration: 1400 });
  };

  const begin = (handle: FrameHandle | "move") => (event: PointerEvent<HTMLElement>) => {
    if (disabled) return;
    // 别让画布把这次按下当成拖画布 / 框选 / 点空白
    event.stopPropagation();
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { handle, startX: event.clientX, startY: event.clientY, start: frame };
  };
  const move = (event: PointerEvent<HTMLElement>) => {
    const current = drag.current;
    if (!current) return;
    apply(
      current.handle,
      current.start,
      (event.clientX - current.startX) / zoom,
      (event.clientY - current.startY) / zoom,
    );
  };
  const end = () => {
    drag.current = null;
  };
  const key = (handle: FrameHandle | "move") => (event: KeyboardEvent<HTMLElement>) => {
    const delta = ARROW_DELTA[event.key];
    if (!delta || disabled) return;
    event.preventDefault();
    const step = event.shiftKey ? KEY_STEP_BIG : KEY_STEP;
    apply(handle, frame, delta[0] * step, delta[1] * step);
  };
  const pointerHandlers = (handle: FrameHandle | "move") => ({
    onPointerDown: begin(handle),
    onPointerMove: move,
    onPointerUp: end,
    onPointerCancel: end,
    onKeyDown: key(handle),
  });

  const checker = checkerStyle(zoom);
  const counter: CSSProperties = { scale: 1 / zoom };
  const slack = fw - origin.w > 0.5 || fh - origin.h > 0.5;

  return (
    <ViewportPortal>
      <div
        className="nodrag nopan nowheel pointer-events-none absolute"
        style={{
          transform: `translate(${px + frame.x0}px, ${py + frame.y0}px)`,
          width: fw,
          height: fh,
        }}
      >
        {/* 四条棋盘格围住原图：上、下、左、右 */}
        <div
          className="absolute"
          style={{ ...checker, left: 0, top: 0, width: fw, height: -frame.y0 }}
        />
        <div
          className="absolute"
          style={{
            ...checker,
            left: 0,
            top: origin.h - frame.y0,
            width: fw,
            height: frame.y1 - origin.h,
          }}
        />
        <div
          className="absolute"
          style={{ ...checker, left: 0, top: -frame.y0, width: -frame.x0, height: origin.h }}
        />
        <div
          className="absolute"
          style={{
            ...checker,
            left: origin.w - frame.x0,
            top: -frame.y0,
            width: frame.x1 - origin.w,
            height: origin.h,
          }}
        />
        <div className="absolute inset-0 rounded-[1px] shadow-[0_0_0_1.5px_white]" />

        {/* 框体：按住拖动平移，盖在原图和棋盘格上，手柄在它上面 */}
        <div
          role="application"
          tabIndex={disabled ? -1 : 0}
          aria-label="移动框（方向键微调，框始终包住原图）"
          className={cn(
            "focus-visible:ring-node-ring/70 pointer-events-auto absolute inset-0 touch-none outline-none focus-visible:ring-2 focus-visible:ring-inset",
            slack && !disabled ? "cursor-grab active:cursor-grabbing" : "cursor-default",
          )}
          {...pointerHandlers("move")}
        />

        {sizeLabel && (
          <span
            className="bg-panel text-foreground ring-chrome-border pointer-events-none absolute left-0 rounded-md px-2 py-px font-mono text-xs whitespace-nowrap tabular-nums ring-1"
            style={{
              ...counter,
              bottom: "100%",
              marginBottom: 8 / zoom,
              transformOrigin: "left bottom",
            }}
          >
            {sizeLabel} <span className="text-muted-foreground">输出</span>
          </span>
        )}

        {HANDLES.map(({ handle, at, cursor, label }) => (
          <div
            key={handle}
            role="slider"
            tabIndex={disabled ? -1 : 0}
            aria-label={label}
            aria-valuetext={`${Math.round(fw)} × ${Math.round(fh)}`}
            className="pointer-events-auto absolute grid size-9 touch-none place-items-center outline-none focus-visible:[&>span]:scale-125"
            style={{
              left: at[0] * fw,
              top: at[1] * fh,
              translate: "-50% -50%",
              cursor: disabled ? "default" : cursor,
              ...counter,
            }}
            {...pointerHandlers(handle)}
          >
            <HandleMark handle={handle} />
          </div>
        ))}
      </div>
    </ViewportPortal>
  );
}
