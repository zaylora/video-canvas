import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 节点的底板：卡片背景、圆角和边框。
 *
 * ReactFlow 把节点套在 `NodeWrapper` 里，编译出来是带 `react-flow__node` 类的 div，
 * 节点选中时那个 div 会多一个 `selected` 类——所以选中态的样式靠 `in-[.selected]:` 往里传。
 */
export function BaseNode({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "bg-card text-card-foreground relative rounded-md border",
        "hover:ring-1",
        "in-[.selected]:border-muted-foreground",
        "in-[.selected]:shadow-lg",
        className,
      )}
      tabIndex={0}
      {...props}
    />
  );
}

/** 节点正文区，配合 `BaseNode` 的内边距使用 */
export function BaseNodeContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="base-node-content"
      className={cn("flex flex-col gap-y-2 p-3", className)}
      {...props}
    />
  );
}
