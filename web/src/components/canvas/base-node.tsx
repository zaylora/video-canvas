import type { ComponentProps } from "react";
import { useNodeId } from "@xyflow/react";

import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";

/**
 * 节点的底板：卡片背景、圆角和描边；选中时描边换成 --node-ring 的中性白（设计稿 6.2）。
 *
 * ReactFlow 把节点套在 `NodeWrapper` 里，编译出来是带 `react-flow__node` 类的 div，
 * 节点选中时那个 div 会多一个 `selected` 类——所以选中态的样式靠 `in-[.selected]:` 往里传。
 */
export function BaseNode({ className, ...props }: ComponentProps<"div">) {
  // Agent 刚改过（紫色）或将被删除（红色）：描边一闪，不改节点数据
  const id = useNodeId();
  const mark = useAgentHighlight((state) => (id ? state.marks[id] : undefined));
  return (
    <div
      data-agent-mark={mark}
      className={cn(
        "bg-card text-card-foreground relative rounded-2xl ring-1 ring-foreground/8 outline-none",
        "transition-[box-shadow] duration-150 hover:ring-foreground/20",
        "in-[.selected]:ring-node-ring in-[.selected]:shadow-2xl in-[.selected]:ring-2",
        "focus-visible:ring-node-ring/60 focus-visible:ring-2",
        "data-[agent-mark=touched]:ring-preset data-[agent-mark=touched]:shadow-2xl data-[agent-mark=touched]:ring-2",
        "data-[agent-mark=danger]:ring-destructive data-[agent-mark=danger]:ring-2",
        className,
      )}
      tabIndex={0}
      {...props}
    />
  );
}

/** 节点正文区：贴满卡片，圆角跟着卡片走 */
export function BaseNodeContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="base-node-content"
      className={cn("flex flex-col gap-y-2 overflow-hidden rounded-[inherit]", className)}
      {...props}
    />
  );
}
