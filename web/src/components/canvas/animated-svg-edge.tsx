import React, { useRef, useState } from "react";
import type { Edge, EdgeProps, Position } from "@xyflow/react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  getStraightPath,
  getSmoothStepPath,
  useReactFlow,
  useStore,
} from "@xyflow/react";
import { Scissors } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";

import { DURATION, EASE_OUT_CSS, TAP, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { cutButtonScale, nearestRatio, shouldShowCutButton } from "@/utils/canvas/edge-cut";
import { isEdgeHighlighted, shouldShowSweep } from "@/utils/canvas/edge-sweep";

export type AnimatedSvgEdge = Edge<{
  /**
   * The amount of time it takes, in seconds, to move the shape one from end of
   * the edge path to the other.
   */
  duration: number;
  /**
   * The direction in which the shape moves along the edge path. Each value
   * corresponds to the following behavior:
   *
   * - `forward`: The shape moves from the source node to the target node.
   *
   * - `reverse`: The shape moves from the target node to the source node.
   *
   * - `alternate`: The shape moves from the source node to the target node and
   *   then back to the source node.
   *
   * - `alternate-reverse`: The shape moves from the target node to the source
   *   node and then back to the target node.
   *
   * If not provided, this defaults to `"forward"`.
   */
  direction?: "forward" | "reverse" | "alternate" | "alternate-reverse";
  /**
   * Which of React Flow's path algorithms to use. Each value corresponds to one
   * of React Flow's built-in edge types.
   *
   * If not provided, this defaults to `"bezier"`.
   */
  path?: "bezier" | "smoothstep" | "step" | "straight";
  /**
   * The number of times to repeat the animation before stopping. If set to
   * `"indefinite"`, the animation will repeat indefinitely.
   *
   * If not provided, this defaults to `"indefinite"`.
   */
  repeat?: number | "indefinite";
  /** 沿路径移动的形状；sweep 不是形状，而是一段沿线扫过的渐变光带 */
  shape: keyof typeof shapes | "sweep";
}>;

/**
 * The `AnimatedSvgEdge` component renders a typical React Flow edge and animates
 * an SVG shape along the edge's path.
 */
export function AnimatedSvgEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data = {
    duration: 2,
    direction: "forward",
    path: "bezier",
    repeat: "indefinite",
    shape: "circle",
  },
  // BaseEdge 最后渲染成 <path>，只认 SVG 属性。EdgeProps 里还有
  // selectable / deletable / sourceHandleId / pathOptions 这些 React Flow 自用的字段，
  // 整包透传会被 React 当成未知 DOM 属性报警告，所以这里显式挑出能用的。
  style,
  markerStart,
  markerEnd,
  interactionWidth,
  label,
  labelStyle,
  labelShowBg,
  labelBgStyle,
  labelBgPadding,
  labelBgBorderRadius,
  // 选中态决定中点那把剪刀露不露面
  selected,
  source,
  target,
}: EdgeProps<AnimatedSvgEdge>) {
  const { deleteElements, screenToFlowPosition } = useReactFlow();
  // 剪刀贴在线上的位置，用 0-1 的路径比例记：在线上按下鼠标时定，节点被拖动后它还在线上；没点过就在中点
  const probe = useRef<SVGPathElement>(null);
  const [cutRatio, setCutRatio] = useState(0.5);
  // 下游在生成：光带换成运行色并加速，一眼看出这根线正在喂数据
  const running = useStore(
    (state) =>
      (state.nodeLookup.get(target)?.data as { status?: string } | undefined)?.status === "running",
  );
  // 两端节点有一个被选中，这根线就算「相关」：换品牌色，流光跟着亮
  const sourceSelected = useStore((state) => !!state.nodeLookup.get(source)?.selected);
  const targetSelected = useStore((state) => !!state.nodeLookup.get(target)?.selected);
  const selectedEdgeCount = useStore((state) => state.edges.filter((edge) => edge.selected).length);
  const selection = { edgeSelected: !!selected, sourceSelected, targetSelected };
  const highlighted = isEdgeHighlighted(selection);
  const showSweep = shouldShowSweep({ ...selection, running });

  // data 为空对象时（比如边没带 data 就被创建）回退到默认形状，避免取不到组件
  const Shape = data.shape === "sweep" ? null : (shapes[data.shape] ?? shapes.circle);

  const [path, labelX, labelY] = getPath({
    type: data.path ?? "bezier",
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  const animateMotionProps = getAnimateMotionProps({
    duration: data.duration,
    direction: data.direction ?? "forward",
    repeat: data.repeat ?? "indefinite",
    path,
  });

  // 在这条线上按下：把按下的点投影到线上，选中后剪刀就出现在鼠标点的位置
  const rememberClick = (event: React.PointerEvent) => {
    const el = probe.current;
    if (!el) return;
    const total = el.getTotalLength();
    const at = screenToFlowPosition({ x: event.clientX, y: event.clientY });
    setCutRatio(nearestRatio((t) => el.getPointAtLength(t * total), at));
  };

  return (
    <>
      {/* 只用来量路径长度和取点，不画也不吃指针 */}
      <path ref={probe} d={path} fill="none" stroke="none" pointerEvents="none" />
      <g onPointerDown={rememberClick}>
        <BaseEdge
          id={id}
          path={path}
          labelX={labelX}
          labelY={labelY}
          className={cn(highlighted && "edge-highlighted")}
          style={style}
          markerStart={markerStart}
          markerEnd={markerEnd}
          interactionWidth={interactionWidth}
          label={label}
          labelStyle={labelStyle}
          labelShowBg={labelShowBg}
          labelBgStyle={labelBgStyle}
          labelBgPadding={labelBgPadding}
          labelBgBorderRadius={labelBgBorderRadius}
        />
      </g>
      {Shape ? (
        <Shape animateMotionProps={animateMotionProps} />
      ) : (
        showSweep && (
          <SweepLight
            id={id}
            path={path}
            from={{ x: sourceX, y: sourceY }}
            to={{ x: targetX, y: targetY }}
            running={running}
            emphasized={!!selected}
          />
        )
      )}
      {shouldShowCutButton(!!selected, selectedEdgeCount) && (
        <CutButton path={path} ratio={cutRatio} onCut={() => deleteElements({ edges: [{ id }] })} />
      )}
    </>
  );
}

/**
 * 选中一条线时浮在线上的「断开」按钮：玻璃小圆钮 + 细线剪刀，点一下断开（⌘Z 可撤销）。
 * 按 React Flow UI 的 button-edge 写法：EdgeLabelRenderer 把内容搬到画布的变换层上；
 * 它的容器不吃指针事件，得由这层定位 div 重新打开，再加 nodrag/nopan 免得点按钮被当成拖画布。
 * 位置用 CSS offset-path 沿这条线按比例摆放，不用在渲染里量 DOM；
 * 大小只轻微跟着画布缩放（见 cutButtonScale），靠反向缩放抵掉画布自己的缩放。
 * 缩放值只在这个组件里订阅，没选中的线不会因为画布缩放而重渲染。
 */
function CutButton({ path, ratio, onCut }: { path: string; ratio: number; onCut: () => void }) {
  const zoom = useStore((state) => state.transform[2]);
  return (
    <EdgeLabelRenderer>
      <div
        className="nodrag nopan pointer-events-auto absolute top-0 left-0"
        style={{
          offsetPath: `path("${path}")`,
          offsetDistance: `${ratio * 100}%`,
          offsetRotate: "0deg",
          transform: `scale(${cutButtonScale(zoom) / zoom})`,
        }}
      >
        <motion.button
          type="button"
          whileTap={TAP}
          aria-label="断开连接"
          title="断开连接 · Delete"
          onClick={onCut}
          style={
            {
              "--motion-in": ms(DURATION.fast),
              "--motion-ease": EASE_OUT_CSS,
            } as React.CSSProperties
          }
          className={cn(
            "bg-chrome text-foreground ring-chrome-border relative grid size-9 place-items-center rounded-full ring-1 backdrop-blur-xl",
            "shadow-lg transition-[background-color,color,scale] duration-(--motion-in) ease-(--motion-ease)",
            "hover:bg-foreground/12 hover:scale-[1.08]",
            "focus-visible:ring-node-ring outline-none focus-visible:ring-2",
            // 点击区域向外扩 4px，缩到最小时也有 36px 左右
            "before:absolute before:-inset-1 before:rounded-full before:content-['']",
            "animate-in fade-in-0 zoom-in-60 duration-(--motion-in) ease-(--motion-ease) motion-reduce:zoom-in-100",
          )}
        >
          <Scissors className="size-[18px]" strokeWidth={1.75} />
        </motion.button>
      </div>
    </EdgeLabelRenderer>
  );
}

/** 光带走完一趟的时长：平时慢、下游生成中快 */
const SWEEP_DURATION = { idle: 2.6, running: 1.1 };

/**
 * 光带流光（设计稿原型「光带」）：一段两头渐隐的亮带沿连线从上游扫到下游。
 * 做法是给连线叠一层渐变描边，渐变方向取上下游连线方向，
 * 再用 animateTransform 把整个渐变从起点外平移到终点外；不移动任何形状，节点挪动时也不会跳帧。
 * 只在选中连线或它两端的节点、或下游生成中时出现（见 shouldShowSweep），静止的画布上只有底线。
 * 系统开了「减少动态效果」时只留静止的底线，不画光带。
 */
function SweepLight({
  id,
  path,
  from,
  to,
  running,
  emphasized,
}: {
  id: string;
  path: string;
  from: { x: number; y: number };
  to: { x: number; y: number };
  running: boolean;
  emphasized: boolean;
}) {
  const reduceMotion = useReducedMotion();
  if (reduceMotion) return null;
  const gradientId = `edge-sweep-${id}`;
  const color = running ? "var(--status-running)" : "var(--edge-shape-color)";
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  return (
    <>
      <defs>
        <linearGradient
          id={gradientId}
          gradientUnits="userSpaceOnUse"
          x1={from.x}
          y1={from.y}
          x2={to.x}
          y2={to.y}
        >
          <stop offset="0.3" style={{ stopColor: color, stopOpacity: 0 }} />
          <stop offset="0.5" style={{ stopColor: color, stopOpacity: emphasized ? 1 : 0.85 }} />
          <stop offset="0.7" style={{ stopColor: color, stopOpacity: 0 }} />
          <animateTransform
            attributeName="gradientTransform"
            type="translate"
            from={`${-dx} ${-dy}`}
            to={`${dx} ${dy}`}
            dur={`${running ? SWEEP_DURATION.running : SWEEP_DURATION.idle}s`}
            repeatCount="indefinite"
          />
        </linearGradient>
      </defs>
      <path
        d={path}
        fill="none"
        stroke={`url(#${gradientId})`}
        strokeWidth={2.5}
        strokeLinecap="round"
        pointerEvents="none"
      />
    </>
  );
}

type AnimateMotionProps = {
  dur: string;
  keyTimes: string;
  keyPoints: string;
  repeatCount: number | "indefinite";
  path: string;
};

type AnimatedSvg = ({
  animateMotionProps,
}: {
  animateMotionProps: AnimateMotionProps;
}) => React.ReactElement;

/** 流星拖尾长度（画布坐标），再长在急弯处会脱离路径 */
const METEOR_TAIL_LENGTH = 34;

const shapes = {
  circle: ({ animateMotionProps }) => (
    <circle r="5" fill="var(--edge-shape-color)">
      <animateMotion {...animateMotionProps} />
    </circle>
  ),

  /**
   * 流动高亮：外层光晕 + 内核亮点一起沿路径移动。
   * 用三层同心圆叠出辉光，比 SVG filter 轻，也不会被边容器裁掉。
   */
  glow: ({ animateMotionProps }) => (
    <g fill="var(--edge-shape-color)">
      <circle r="16" opacity="0.12" />
      <circle r="10" opacity="0.25" />
      <circle r="5.5" opacity="0.55" />
      <circle r="2.5" fill="var(--edge-shape-core, #fff)" />
      <animateMotion {...animateMotionProps} />
    </g>
  ),

  meteor: Meteor,

  package: ({ animateMotionProps }) => (
    <g fill="#dfc7b1" stroke="#2b2a2a" transform="translate(-10,-10)">
      <path d="M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z" />
      <path d="M12 22V12" />
      <path d="m3.3 7 7.703 4.734a2 2 0 0 0 1.994 0L20.7 7" />
      <path d="m7.5 4.27 9 5.15" />
      <animateMotion {...animateMotionProps} />
    </g>
  ),
} satisfies Record<string, AnimatedSvg>;

/**
 * Chooses which of React Flow's edge path algorithms to use based on the provided
 * `type`.
 */
function getPath({
  type,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
}: {
  type: "bezier" | "smoothstep" | "step" | "straight";
  sourceX: number;
  sourceY: number;
  targetX: number;
  targetY: number;
  sourcePosition: Position;
  targetPosition: Position;
}) {
  switch (type) {
    case "bezier":
      return getBezierPath({
        sourceX,
        sourceY,
        targetX,
        targetY,
        sourcePosition,
        targetPosition,
      });

    case "smoothstep":
      return getSmoothStepPath({
        sourceX,
        sourceY,
        targetX,
        targetY,
        sourcePosition,
        targetPosition,
      });

    case "step":
      return getSmoothStepPath({
        sourceX,
        sourceY,
        targetX,
        targetY,
        sourcePosition,
        targetPosition,
        borderRadius: 0,
      });

    case "straight":
      return getStraightPath({
        sourceX,
        sourceY,
        targetX,
        targetY,
      });
  }
}

/**
 * Construct the props for an `<animateMotion />` element based on an
 * `AnimatedSvgEdge`'s data.
 */
function getAnimateMotionProps({
  duration,
  direction,
  repeat,
  path,
}: {
  duration: number;
  direction: "forward" | "reverse" | "alternate" | "alternate-reverse";
  repeat: number | "indefinite";
  path: string;
}) {
  const base = {
    path,
    repeatCount: repeat,
    // The default calcMode for the `<animateMotion />` element is "paced", which
    // is not compatible with the `keyPoints` attribute. Setting this to "linear"
    // ensures that the shape correct follows the path.
    calcMode: "linear",
  };

  switch (direction) {
    case "forward":
      return {
        ...base,
        dur: `${duration}s`,
        keyTimes: "0;1",
        keyPoints: "0;1",
      };

    case "reverse":
      return {
        ...base,
        dur: `${duration}s`,
        keyTimes: "0;1",
        keyPoints: "1;0",
      };

    case "alternate":
      return {
        ...base,
        // By doubling the animation duration, the time spent moving from one end
        // to the other remains consistent when switching between directions.
        dur: `${duration * 2}s`,
        keyTimes: "0;0.5;1",
        keyPoints: "0;1;0",
      };

    case "alternate-reverse":
      return {
        ...base,
        dur: `${duration * 2}s`,
        keyTimes: "0;0.5;1",
        keyPoints: "1;0;1",
      };
  }
}

/**
 * 流星：亮核在前，尾焰在后渐隐。
 *
 * animateMotion 的 rotate="auto" 会让整组跟着路径切线转，尾巴因此始终甩在
 * 运动的反方向；反向播放时切线方向不会随 keyPoints 一起翻转，得换成
 * "auto-reverse"，否则流星是倒着飞的。
 */
function Meteor({ animateMotionProps }: { animateMotionProps: AnimateMotionProps }) {
  // 渐变靠 id 引用，用 useId 保证多条边同时跑也不会互相顶掉
  const tailId = React.useId();
  const reversed = animateMotionProps.keyPoints.startsWith("1");
  const tail = `M ${-METEOR_TAIL_LENGTH} 0 L 0 0`;

  return (
    <g>
      <defs>
        {/* userSpaceOnUse + 负向 x1：渐变直接按尾巴的本地坐标铺，头亮尾透 */}
        <linearGradient
          id={tailId}
          gradientUnits="userSpaceOnUse"
          x1={-METEOR_TAIL_LENGTH}
          y1="0"
          x2="0"
          y2="0"
        >
          <stop offset="0%" stopColor="var(--edge-shape-color)" stopOpacity="0" />
          <stop offset="60%" stopColor="var(--edge-shape-color)" stopOpacity="0.45" />
          <stop offset="100%" stopColor="var(--edge-shape-color)" stopOpacity="1" />
        </linearGradient>
      </defs>
      {/* 外层宽尾当辉光，内层细尾当焰心，叠出通透的拖尾 */}
      <path
        d={tail}
        fill="none"
        stroke={`url(#${tailId})`}
        strokeWidth="9"
        strokeLinecap="round"
        opacity="0.2"
      />
      <path d={tail} fill="none" stroke={`url(#${tailId})`} strokeWidth="3" strokeLinecap="round" />
      <g fill="var(--edge-shape-color)">
        <circle r="7" opacity="0.18" />
        <circle r="4" opacity="0.4" />
        <circle r="2" fill="var(--edge-shape-core, #fff)" />
      </g>
      <animateMotion {...animateMotionProps} rotate={reversed ? "auto-reverse" : "auto"} />
    </g>
  );
}
