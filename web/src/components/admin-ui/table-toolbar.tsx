import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/** 表格上方的工具条：搜索、筛选、计数排成一行，放不下自动换行 */
function TableToolbar({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="table-toolbar"
      className={cn("flex flex-wrap items-center gap-2 p-4", className)}
      {...props}
    />
  );
}

/** 工具条右侧的“筛选后 / 总数”计数 */
function TableToolbarCount({
  shown,
  total,
  className,
}: {
  shown: number;
  total: number;
  className?: string;
}) {
  return (
    <span
      data-slot="table-toolbar-count"
      className={cn("text-muted-foreground ml-auto text-xs tabular-nums", className)}
    >
      {shown} / {total}
    </span>
  );
}

export { TableToolbar, TableToolbarCount };
