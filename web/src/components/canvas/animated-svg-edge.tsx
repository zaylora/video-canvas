import React from "react";
import type { Edge, EdgeProps, Position } from "@xyflow/react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  getStraightPath,
  getSmoothStepPath,
  useReactFlow,
} from "@xyflow/react";
import { Scissors } from "lucide-react";

import { Button } from "@/components/ui/button";

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
  shape: keyof typeof shapes;
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
}: EdgeProps<AnimatedSvgEdge>) {
  const { deleteElements } = useReactFlow();

  // data 为空对象时（比如边没带 data 就被创建）回退到默认形状，避免取不到组件
  const Shape = shapes[data.shape] ?? shapes.circle;

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

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        labelX={labelX}
        labelY={labelY}
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
      <Shape animateMotionProps={animateMotionProps} />
      {/* 选中这条线就在中点浮出剪刀，点一下断开关联。
          按 React Flow UI 的 button-edge 写法：EdgeLabelRenderer 把内容搬到画布的
          变换层上，坐标直接用画布坐标；它的容器不吃指针事件，得由这层定位 div
          重新打开，再加 nodrag/nopan 免得点按钮被当成拖画布 */}
      {selected && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan pointer-events-auto absolute"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            }}
          >
            <Button
              aria-label="断开连接"
              title="断开连接"
              size="icon-sm"
              variant="secondary"
              className="hover:text-destructive shadow-sm"
              onClick={() => deleteElements({ edges: [{ id }] })}
            >
              <Scissors />
            </Button>
          </div>
        </EdgeLabelRenderer>
      )}
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
    <circle r="5" fill="var(--edge-shape-color, #ff0073)">
      <animateMotion {...animateMotionProps} />
    </circle>
  ),

  /**
   * 流动高亮：外层光晕 + 内核亮点一起沿路径移动。
   * 用三层同心圆叠出辉光，比 SVG filter 轻，也不会被边容器裁掉。
   */
  glow: ({ animateMotionProps }) => (
    <g fill="var(--edge-shape-color, #ff0073)">
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
          <stop offset="0%" stopColor="var(--edge-shape-color, #ff0073)" stopOpacity="0" />
          <stop offset="60%" stopColor="var(--edge-shape-color, #ff0073)" stopOpacity="0.45" />
          <stop offset="100%" stopColor="var(--edge-shape-color, #ff0073)" stopOpacity="1" />
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
      <g fill="var(--edge-shape-color, #ff0073)">
        <circle r="7" opacity="0.18" />
        <circle r="4" opacity="0.4" />
        <circle r="2" fill="var(--edge-shape-core, #fff)" />
      </g>
      <animateMotion {...animateMotionProps} rotate={reversed ? "auto-reverse" : "auto"} />
    </g>
  );
}
