import { useRef, type CSSProperties, type KeyboardEvent, type PointerEvent } from "react";
import { motion, useReducedMotion } from "motion/react";

import { DURATION, EASE_OUT, EASE_OUT_CSS, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  OUTPAINT_HANDLES,
  frameHeight,
  frameWidth,
  moveFrame,
  outputSize,
  resizeFrame,
  type ImageSize,
  type OutpaintFrame,
  type OutpaintHandle,
} from "@/utils/canvas/outpaint";

/** 框要盖在节点（选中的节点会被抬高）上面 */
const FRAME_Z_INDEX = 2000;
/** 方向键微调一次走多少屏幕像素，按住 Shift 走大步 */
const NUDGE = 8;
const NUDGE_BIG = 32;

/** 手柄的样子：可点范围、画出来的形状，单位是屏幕像素（调用方除以画布缩放后再用） */
const HANDLE_HIT: Record<OutpaintHandle, CSSProperties> = {
  l: { left: -10, top: "50%", width: 20, height: 40, marginTop: -20, cursor: "ew-resize" },
  r: { right: -10, top: "50%", width: 20, height: 40, marginTop: -20, cursor: "ew-resize" },
  t: { top: -10, left: "50%", width: 40, height: 20, marginLeft: -20, cursor: "ns-resize" },
  b: { bottom: -10, left: "50%", width: 40, height: 20, marginLeft: -20, cursor: "ns-resize" },
  tl: { left: -14, top: -14, width: 28, height: 28, cursor: "nwse-resize" },
  br: { right: -14, bottom: -14, width: 28, height: 28, cursor: "nwse-resize" },
  tr: { right: -14, top: -14, width: 28, height: 28, cursor: "nesw-resize" },
  bl: { left: -14, bottom: -14, width: 28, height: 28, cursor: "nesw-resize" },
};

/** 手柄画出来的部分：边是短条，角是 L 形折角；位置相对可点范围 */
const HANDLE_ART: Record<OutpaintHandle, CSSProperties> = {
  l: { left: 8, top: 8, width: 4, height: 24, borderRadius: 2 },
  r: { left: 8, top: 8, width: 4, height: 24, borderRadius: 2 },
  t: { left: 8, top: 8, width: 24, height: 4, borderRadius: 2 },
  b: { left: 8, top: 8, width: 24, height: 4, borderRadius: 2 },
  tl: { left: 9, top: 9, width: 14, height: 14, borderStyle: "solid", borderWidth: "3px 0 0 3px" },
  tr: { right: 9, top: 9, width: 14, height: 14, borderStyle: "solid", borderWidth: "3px 3px 0 0" },
  bl: {
    left: 9,
    bottom: 9,
    width: 14,
    height: 14,
    borderStyle: "solid",
    borderWidth: "0 0 3px 3px",
  },
  br: {
    right: 9,
    bottom: 9,
    width: 14,
    height: 14,
    borderStyle: "solid",
    borderWidth: "0 3px 3px 0",
  },
};

const HANDLE_LABEL: Record<OutpaintHandle, string> = {
  l: "左边",
  r: "右边",
  t: "上边",
  b: "下边",
  tl: "左上角（等比）",
  tr: "右上角（等比）",
  bl: "左下角（等比）",
  br: "右下角（等比）",
};

/** 把一个样式里的数字（屏幕像素）都除以缩放，字符串（百分比等）原样保留 */
function scaled(style: CSSProperties, zoom: number): CSSProperties {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(style)) {
    out[key] =
      typeof value === "number" && key !== "opacity" && key !== "zIndex" ? value / zoom : value;
  }
  return out as CSSProperties;
}

type OutpaintFrameProps = {
  /** 原图的像素尺寸 */
  size: ImageSize;
  /** 外框（原图像素，原图左上角为原点） */
  frame: OutpaintFrame;
  /** 原图左上角在画布上的位置（画布坐标） */
  origin: { x: number; y: number };
  /** 原图一个像素对应多少画布单位 */
  k: number;
  /** 画布缩放：手柄、线宽、文字要抵消它，才能在屏幕上保持固定大小 */
  zoom: number;
  /** 切换比例 / 倍数时带一段补间；拖动时不补间，要跟手 */
  animate: boolean;
  onChange: (frame: OutpaintFrame) => void;
  /** 想超过 3 倍上限、被拦住了 */
  onLimit: () => void;
};

/** 正在进行的一次拖动：起点、开始时的框、拖的是手柄还是整个框 */
type Drag = {
  mode: OutpaintHandle | "move";
  startX: number;
  startY: number;
  start: OutpaintFrame;
};

/**
 * 扩图的框（设计稿 6.17）：画在节点周围，框内是棋盘格（= 透明 = 要生成的区域），
 * 原图所在处挖空，露出下面的节点；八个手柄改范围，按住框体可以平移，框始终包住原图。
 * 渲染在画布的 ViewportPortal 里，跟着画布平移缩放；手柄、线宽、文字除以缩放，屏幕上大小固定。
 * 拖动用 pointer 事件加指针捕获，位移除以「缩放 × 显示比例」换回原图像素再交给几何函数。
 */
export function OutpaintFrame({
  size,
  frame,
  origin,
  k,
  zoom,
  animate,
  onChange,
  onLimit,
}: OutpaintFrameProps) {
  const reduce = useReducedMotion();
  const drag = useRef<Drag | null>(null);
  const u = (px: number) => px / zoom;

  const left = origin.x + frame.x0 * k;
  const top = origin.y + frame.y0 * k;
  const width = frameWidth(frame) * k;
  const height = frameHeight(frame) * k;
  // 原图在框里的位置，用来挖洞
  const hole = { x: -frame.x0 * k, y: -frame.y0 * k, w: size.width * k, h: size.height * k };
  const clip = `polygon(evenodd, 0 0, ${width}px 0, ${width}px ${height}px, 0 ${height}px, 0 0, ${hole.x}px ${hole.y}px, ${hole.x}px ${hole.y + hole.h}px, ${hole.x + hole.w}px ${hole.y + hole.h}px, ${hole.x + hole.w}px ${hole.y}px, ${hole.x}px ${hole.y}px)`;
  const canMove = frameWidth(frame) - size.width > 0.5 || frameHeight(frame) - size.height > 0.5;
  const out = outputSize(frame);

  const tween = animate && !reduce;
  const tweenStyle = tween
    ? (["left", "top", "width", "height", "clip-path"] as const)
        .map((prop) => `${prop} ${ms(DURATION.base)} ${EASE_OUT_CSS}`)
        .join(", ")
    : undefined;

  /** 按拖动模式算新框；返回是否撞到上限 */
  const apply = (mode: Drag["mode"], start: OutpaintFrame, dx: number, dy: number) => {
    if (mode === "move") {
      onChange(moveFrame(size, start, dx, dy));
      return;
    }
    const result = resizeFrame(size, mode, start, dx, dy);
    onChange(result.frame);
    if (result.limited) onLimit();
  };

  const begin = (mode: Drag["mode"]) => (event: PointerEvent<HTMLElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { mode, startX: event.clientX, startY: event.clientY, start: frame };
    event.preventDefault();
    event.stopPropagation();
  };
  const move = (event: PointerEvent<HTMLElement>) => {
    const d = drag.current;
    if (!d) return;
    const scale = zoom * k;
    apply(d.mode, d.start, (event.clientX - d.startX) / scale, (event.clientY - d.startY) / scale);
  };
  const end = () => {
    drag.current = null;
  };
  const nudge = (mode: Drag["mode"]) => (event: KeyboardEvent<HTMLElement>) => {
    const dir = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[
      event.key
    ];
    if (!dir) return;
    event.preventDefault();
    event.stopPropagation();
    const step = (event.shiftKey ? NUDGE_BIG : NUDGE) / (zoom * k);
    apply(mode, frame, dir[0] * step, dir[1] * step);
  };

  return (
    <motion.div
      // nopan nodrag nowheel：在框上拖、滚都不能带动画布或节点。
      // pointer-events-auto：xyflow 的视口容器是 pointer-events: none，ViewportPortal 里的内容继承了它，
      // 不显式打开，框和手柄就收不到任何指针事件
      className="nopan nodrag nowheel pointer-events-auto absolute"
      style={{ left, top, width, height, zIndex: FRAME_Z_INDEX, transition: tweenStyle }}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1, transition: { duration: DURATION.base, ease: EASE_OUT } }}
      exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
    >
      {/* 棋盘格：由前景色和画布底色混出两档灰，深浅主题都跟着变 */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          clipPath: clip,
          backgroundColor: "color-mix(in oklch, var(--foreground) 5%, var(--canvas-bg))",
          backgroundImage:
            "conic-gradient(color-mix(in oklch, var(--foreground) 10%, var(--canvas-bg)) 25%, transparent 0 50%, color-mix(in oklch, var(--foreground) 10%, var(--canvas-bg)) 0 75%, transparent 0)",
          backgroundSize: `${u(24)}px ${u(24)}px`,
          transition: tweenStyle,
        }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{ boxShadow: `0 0 0 ${u(1.5)}px var(--cover-foreground)` }}
      />

      {/* 框体：按住拖动 = 平移；盖在原图和棋盘格上，手柄在它上面 */}
      <div
        role="button"
        tabIndex={0}
        aria-label="移动框（方向键微调，框始终包住原图）"
        className={cn(
          "focus-visible:ring-node-ring/60 absolute inset-0 touch-none outline-none focus-visible:ring-2",
          canMove ? "cursor-grab active:cursor-grabbing" : "cursor-default",
        )}
        onPointerDown={begin("move")}
        onPointerMove={move}
        onPointerUp={end}
        onPointerCancel={end}
        onKeyDown={nudge("move")}
      />

      {OUTPAINT_HANDLES.map((handle) => (
        <div
          key={handle}
          role="button"
          tabIndex={0}
          aria-label={`调整${HANDLE_LABEL[handle]}（方向键微调）`}
          className="group absolute touch-none outline-none"
          style={scaled(HANDLE_HIT[handle], zoom)}
          onPointerDown={begin(handle)}
          onPointerMove={move}
          onPointerUp={end}
          onPointerCancel={end}
          onKeyDown={nudge(handle)}
        >
          <span
            className={cn(
              "absolute transition-transform group-hover:scale-[1.18] group-focus-visible:scale-[1.18]",
              handle.length === 1 ? "bg-cover-foreground" : "border-cover-foreground",
            )}
            style={{
              ...scaled(HANDLE_ART[handle], zoom),
              transitionDuration: ms(DURATION.fast),
              transitionTimingFunction: EASE_OUT_CSS,
              filter: "drop-shadow(0 0 1px var(--stage-scrim-edge))",
            }}
          />
        </div>
      ))}

      {/* 输出尺寸：框左上外侧，随框实时变化 */}
      <div
        className="bg-panel text-foreground pointer-events-none absolute left-0 whitespace-nowrap tabular-nums"
        style={{
          bottom: `calc(100% + ${u(8)}px)`,
          fontSize: u(12),
          lineHeight: 1.5,
          padding: `0 ${u(8)}px`,
          borderRadius: u(6),
          boxShadow: `0 0 0 ${u(1)}px var(--chrome-border)`,
        }}
      >
        {out.width} × {out.height} <span className="text-muted-foreground">输出</span>
      </div>
    </motion.div>
  );
}
