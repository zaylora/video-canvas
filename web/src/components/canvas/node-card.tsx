import type { ReactNode } from "react";
import { Handle, Position, type HandleProps } from "@xyflow/react";
import { Plus } from "lucide-react";
import { motion } from "motion/react";

import { cn } from "@/lib/utils";

import { BaseNode } from "./base-node";
import {
  TILT_PERSPECTIVE,
  useConnectionTilt,
  type ConnectionTiltOptions,
} from "./hooks/use-connection-tilt";

/**
 * 节点连接点的位置描述，只取 Handle 里和摆放有关的几项。
 * 同一侧有多个连接点时（比如视频模型的多个输入口）用 top 错开，label 是口的名字。
 */
export type NodeCardHandle = Pick<HandleProps, "type" | "position" | "id"> & {
  /** 沿边的位置（CSS top，仅左右两侧生效），缺省居中 */
  top?: string;
  /** 输入口的名字，悬停节点时显示在口旁边 */
  label?: string;
  /** 同侧口多时缩小命中区，免得互相盖住 */
  compact?: boolean;
};

/** 缺省的连接点：左进右出 */
const DEFAULT_HANDLES: NodeCardHandle[] = [
  { type: "target", position: Position.Left },
  { type: "source", position: Position.Right },
];

/**
 * 连接点的长相：xyflow 拿 handle 盒子的外沿当连线端点（右侧取右边缘、左侧取左边缘），
 * 把 handle 本身撑大会把线头推离节点，所以盒子保持 xyflow 默认的那几像素、只抹掉可见样式，
 * 命中区改由 ::before 铺开——伪元素照样吃指针事件，却不进 getBoundingClientRect，端点纹丝不动。
 */
const HANDLE_BASE_CLASS = cn(
  "pointer-events-auto rounded-none border-0 bg-transparent",
  "before:absolute before:top-1/2 before:left-1/2 before:-translate-x-1/2 before:-translate-y-1/2 before:content-['']",
);

/** 按朝向铺开命中区：左右两侧竖着铺，上下两侧横着铺 */
const HANDLE_AXIS_CLASS: Record<Position, string> = {
  [Position.Left]: "before:h-48 before:w-14",
  [Position.Right]: "before:h-48 before:w-14",
  [Position.Top]: "before:h-14 before:w-48",
  [Position.Bottom]: "before:h-14 before:w-48",
};

/**
 * 图标的默认落点：以 handle 盒子中心（也就是节点边框上）为原点，
 * 往节点外侧推一个图标半径，图标就贴在边框外面挨着节点，不压节点内容。
 */
const HANDLE_ICON_HOME: Record<Position, { left: string; top: string }> = {
  [Position.Left]: { left: "calc(50% - 14px)", top: "50%" },
  [Position.Right]: { left: "calc(50% + 14px)", top: "50%" },
  [Position.Top]: { left: "50%", top: "calc(50% - 14px)" },
  [Position.Bottom]: { left: "50%", top: "calc(50% + 14px)" },
};

/**
 * 单个连接点：一条大命中区加一个 ⊕ 图标，
 * 图标平时不露面，鼠标移到节点上或节点选中时才浮出来，默认贴着节点的边；
 * 指针进了命中区就跟着指针跑，指到哪就提示能从哪拉线，离开再归位贴边。
 */
function NodeCardHandleDot({ type, position, id, top, label, compact }: NodeCardHandle) {
  const home = HANDLE_ICON_HOME[position];

  // 指针一动就要挪图标，走 state 会把整个节点带着重渲染，这里直接改 CSS 变量。
  // 坐标相对 handle 盒子算，指针跑在伪元素铺开的那一圈里时值会超出盒子，正是要的效果
  const trackPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    const { style } = event.currentTarget;
    style.setProperty("--handle-x", `${event.clientX - rect.left}px`);
    style.setProperty("--handle-y", `${event.clientY - rect.top}px`);
  };

  // 指针离开就把图标交还给条的正中间，也就是节点边框上
  const resetPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const { style } = event.currentTarget;
    style.removeProperty("--handle-x");
    style.removeProperty("--handle-y");
  };

  return (
    <Handle
      type={type}
      position={position}
      id={id}
      style={top ? { top } : undefined}
      className={cn(HANDLE_BASE_CLASS, HANDLE_AXIS_CLASS[position], compact && "before:h-10")}
      onPointerMove={trackPointer}
      onPointerLeave={resetPointer}
    >
      <div
        className={cn(
          "bg-canvas text-muted-foreground ring-foreground/30 pointer-events-none absolute flex size-7 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full ring-[1.5px]",
          // 平时藏起来，鼠标上了节点或节点被选中才从 0.6 倍弹出来。
          // hover 认的是 NodeCard 上的 group/node，命中条是节点的子元素，
          // 所以扫到伸出节点外的那半截也算悬浮在节点上
          "scale-60 opacity-0 transition-[opacity,scale] duration-200 ease-[cubic-bezier(0.2,0,0,1)]",
          "group-hover/node:scale-100 group-hover/node:opacity-100 in-[.selected]:scale-100 in-[.selected]:opacity-100",
        )}
        style={{
          left: `var(--handle-x, ${home.left})`,
          top: `var(--handle-y, ${home.top})`,
        }}
      >
        <Plus className="size-3.5" />
      </div>
      {label && (
        <span
          className={cn(
            "bg-card/90 text-muted-foreground pointer-events-none absolute -translate-y-1/2 rounded-md border px-1.5 py-0.5 text-[10px] whitespace-nowrap",
            "opacity-0 transition-opacity group-hover/node:opacity-100 in-[.selected]:opacity-100",
            position === Position.Left ? "right-full mr-9" : "left-full ml-9",
          )}
          style={{ top: "50%" }}
        >
          {label}
        </span>
      )}
    </Handle>
  );
}

type NodeCardProps = {
  /** 节点标题，摆在卡片上方，同时是无障碍名称 */
  title: string;
  /** 标题左边的种类图标 */
  icon?: ReactNode;
  /** 标题行右侧的状态（生成中 42%、生成失败） */
  status?: ReactNode;
  /** 连接点配置，缺省为左 target、右 source */
  handles?: NodeCardHandle[];
  /** 拉过来的线接不接得上，用来决定要不要给倾斜反馈；缺省一律接 */
  canAcceptConnection?: ConnectionTiltOptions["canAccept"];
  className?: string;
  /** 节点正文，通常是一个 BaseNodeContent */
  children: ReactNode;
};

/**
 * 画布节点的通用外壳：正文插槽 + 连接点。
 * 只管长相，不认识任何节点种类，正文与动作全由调用方装配。
 */
export function NodeCard({
  title,
  icon,
  status,
  handles = DEFAULT_HANDLES,
  canAcceptConnection,
  className,
  children,
}: NodeCardProps) {
  const { rotateX, rotateY, scale } = useConnectionTilt({
    canAccept: canAcceptConnection,
  });

  return (
    // 外面这层只管 3D：别人拉线压到本节点身上时朝鼠标偏一点头，
    // 倾斜留在包装层，BaseNode 里连接点的绝对定位和 .selected 样式都不受影响
    <motion.div style={{ transformPerspective: TILT_PERSPECTIVE, rotateX, rotateY, scale }}>
      <BaseNode aria-label={title} className={cn("group/node w-96", className)}>
        <div className="text-foreground pointer-events-none absolute right-0.5 bottom-full left-0.5 mb-2 flex items-center gap-2 text-[13px] font-semibold">
          {icon && <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>}
          <span className="truncate">{title}</span>
          {status && <span className="ml-auto shrink-0 text-xs font-medium">{status}</span>}
        </div>
        {children}
        {handles.map((handle) => (
          <NodeCardHandleDot
            key={`${handle.type}-${handle.position}-${handle.id ?? ""}`}
            type={handle.type}
            position={handle.position}
            id={handle.id}
            top={handle.top}
            label={handle.label}
            compact={handle.compact}
          />
        ))}
      </BaseNode>
    </motion.div>
  );
}
