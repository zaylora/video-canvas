import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 表格上方的工具栏（设计稿样式）：搜索、分段筛选、下拉筛选靠左，计数用 ml-auto 放到最右。
 * <DataTableToolbar><SearchInput /><Segmented />…<span className="ml-auto" /></DataTableToolbar>
 */
function DataTableToolbar({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="data-table-toolbar"
      className={cn("flex flex-wrap items-center gap-2 p-4", className)}
      {...props}
    />
  );
}

export { DataTableToolbar };
