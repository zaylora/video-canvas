import { useState, type ReactNode } from "react";
import { Handle, Position, type HandleProps } from "@xyflow/react";
import { Plus } from "lucide-react";
import { motion } from "motion/react";

import { cn } from "@/lib/utils";
import { NODE_LABEL_MAX, normalizeNodeLabel } from "@/utils/canvas/node-label";

import { BaseNode } from "./base-node";
import { useConnectionTilt, type ConnectionTiltOptions } from "./hooks/use-connection-tilt";

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
  "before:absolute before:content-['']",
);

/**
 * 按朝向铺开命中区：沿边方向居中铺开，朝外一侧伸出去，朝内一侧不进节点，
 * 免得盖住节点里的控制条、按钮这类要点的东西。
 */
const HANDLE_AXIS_CLASS: Record<Position, string> = {
  [Position.Left]:
    "before:top-1/2 before:left-1/2 before:h-48 before:w-7 before:-translate-x-full before:-translate-y-1/2",
  [Position.Right]: "before:top-1/2 before:left-1/2 before:h-48 before:w-7 before:-translate-y-1/2",
  [Position.Top]:
    "before:top-1/2 before:left-1/2 before:h-7 before:w-48 before:-translate-x-1/2 before:-translate-y-full",
  [Position.Bottom]:
    "before:top-1/2 before:left-1/2 before:h-7 before:w-48 before:-translate-x-1/2",
};

/** 图标贴边时离边框的距离：一个图标半径 */
const ICON_OFFSET = "14px";

/**
 * 图标的默认落点：以 handle 盒子中心（也就是节点边框上）为原点，
 * 往节点外侧推一个图标半径，图标就贴在边框外面挨着节点，不压节点内容。
 */
const HANDLE_ICON_HOME: Record<Position, { left: string; top: string }> = {
  [Position.Left]: { left: `calc(50% - ${ICON_OFFSET})`, top: "50%" },
  [Position.Right]: { left: `calc(50% + ${ICON_OFFSET})`, top: "50%" },
  [Position.Top]: { left: "50%", top: `calc(50% - ${ICON_OFFSET})` },
  [Position.Bottom]: { left: "50%", top: `calc(50% + ${ICON_OFFSET})` },
};

/**
 * 图标跟随指针时的坐标：朝外那根轴上夹在贴边位置之外，永远不会跑进节点里压住内容；
 * 沿边那根轴照常跟着指针。
 */
function iconPosition(position: Position): { left: string; top: string } {
  const home = HANDLE_ICON_HOME[position];
  const x = `var(--handle-x, ${home.left})`;
  const y = `var(--handle-y, ${home.top})`;
  switch (position) {
    case Position.Left:
      return { left: `min(${x}, ${home.left})`, top: y };
    case Position.Right:
      return { left: `max(${x}, ${home.left})`, top: y };
    case Position.Top:
      return { left: x, top: `min(${y}, ${home.top})` };
    case Position.Bottom:
      return { left: x, top: `max(${y}, ${home.top})` };
  }
}

/**
 * 单个连接点：一条大命中区加一个 ⊕ 图标，
 * 图标平时不露面，鼠标移到节点上或节点选中时才浮出来，默认贴着节点的边；
 * 指针进了命中区就沿边跟着指针跑，指到哪就提示能从哪拉线（不会跑进节点里），离开再归位贴边。
 */
function NodeCardHandleDot({ type, position, id, top, label, compact }: NodeCardHandle) {
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
        style={iconPosition(position)}
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

/**
 * 节点标题：双击原地变输入框改名（设计稿 6.2），Enter / 失焦提交，Esc 取消。
 * 名字也是提示词里 @ 素材时搜索、显示用的那个。不给 onRename 就只读。
 */
function NodeTitle({ title, onRename }: { title: string; onRename?: (label: string) => void }) {
  const [draft, setDraft] = useState<string | null>(null);

  if (draft !== null)
    return (
      <input
        autoFocus
        value={draft}
        maxLength={NODE_LABEL_MAX * 2}
        aria-label="节点名称"
        onFocus={(event) => event.currentTarget.select()}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === "Enter") event.currentTarget.blur();
          if (event.key === "Escape") {
            event.stopPropagation();
            setDraft(null);
          }
        }}
        onBlur={() => {
          const next = normalizeNodeLabel(draft, title);
          setDraft(null);
          if (next !== title) onRename?.(next);
        }}
        // 选字、拖光标不能把节点或画布拖走
        className="nodrag nopan bg-background ring-node-ring/60 pointer-events-auto -mx-1 h-6 min-w-0 flex-1 rounded-md px-1 text-[13px] font-semibold ring-1 outline-none"
      />
    );

  return (
    <span
      title={onRename ? "双击重命名" : undefined}
      onDoubleClick={(event) => {
        if (!onRename) return;
        // 别让画布把这次双击当成「在空白处新建节点」
        event.stopPropagation();
        setDraft(title);
      }}
      className={cn("truncate", onRename && "pointer-events-auto cursor-text")}
    >
      {title}
    </span>
  );
}

type NodeCardProps = {
  /** 节点标题，摆在卡片上方，同时是无障碍名称 */
  title: string;
  /** 双击标题改名后回调，参数已经规整过；不给就不能改名 */
  onRename?: (label: string) => void;
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
  onRename,
  icon,
  status,
  handles = DEFAULT_HANDLES,
  canAcceptConnection,
  className,
  children,
}: NodeCardProps) {
  const { rotateX, rotateY, scale, opacity, transformPerspective } = useConnectionTilt({
    canAccept: canAcceptConnection,
  });

  return (
    // 外面这层只管 3D：别人拉线压到本节点身上时朝鼠标偏一点头，
    // 倾斜留在包装层，BaseNode 里连接点的绝对定位和 .selected 样式都不受影响；
    // 透视只在倾斜时才有，静止的节点不做 3D 变换、不占独立合成层
    <motion.div style={{ transformPerspective, rotateX, rotateY, scale, opacity }}>
      <BaseNode aria-label={title} className={cn("group/node w-xl", className)}>
        <div className="text-foreground pointer-events-none absolute right-0.5 bottom-full left-0.5 mb-2 flex items-center gap-2 text-[13px] font-semibold">
          {icon && <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>}
          <NodeTitle title={title} onRename={onRename} />
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
