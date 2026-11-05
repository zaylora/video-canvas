import type { ComponentProps } from "react";

import { DataTablePagination } from "@/components/admin-ui/data-table-pagination";
import { GLASS_PANEL } from "@/components/admin-ui/glass";
import { cn } from "@/lib/utils";

/**
 * 表格页的整页骨架：标题区固定在上，下面的 DataPanel 占满剩余高度，页面本身不滚动。
 * <DataPage>
 *   <PageHeader />
 *   <DataPanel>
 *     <TableToolbar />
 *     <DataPanelBody><Table /></DataPanelBody>
 *     <DataPanelPagination page={…} />
 *   </DataPanel>
 * </DataPage>
 */
function DataPage({ className, ...props }: ComponentProps<"main">) {
  return (
    <main
      data-slot="data-page"
      className={cn(
        "flex h-full min-h-0 flex-col overflow-y-auto px-4 pt-6 pb-4 lg:px-6",
        className,
      )}
      {...props}
    />
  );
}

/** 表格卡片（清透面板）：纵向 flex，工具条、滚动区、分页条依次排开，高度由外层给定 */
function DataPanel({ className, ...props }: ComponentProps<"section">) {
  return (
    <section
      data-slot="data-panel"
      className={cn(GLASS_PANEL, "flex min-h-64 flex-1 flex-col overflow-hidden", className)}
      {...props}
    />
  );
}

/**
 * 表格卡片中间的滚动区：内容超出时只在这里滚动；
 * 表头吸顶，Table 自带的横向滚动容器让位给这一层（否则吸顶会失效）。
 */
function DataPanelBody({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="data-panel-body"
      className={cn(
        "border-chrome-border min-h-0 flex-1 overflow-auto border-t",
        "**:data-[slot=table-container]:overflow-visible",
        "[&_[data-slot=table-header]_th]:bg-chrome [&_[data-slot=table-header]_th]:backdrop-blur-xl [&_[data-slot=table-header]_th]:sticky [&_[data-slot=table-header]_th]:top-0 [&_[data-slot=table-header]_th]:z-10 [&_[data-slot=table-header]_th]:shadow-[inset_0_-1px_0_var(--chrome-border)]",
        className,
      )}
      {...props}
    />
  );
}

/** 表格卡片底部的分页条：始终贴在卡片底部，不随表格滚动；参数同 DataTablePagination */
function DataPanelPagination({ className, ...props }: ComponentProps<typeof DataTablePagination>) {
  return (
    <div data-slot="data-panel-footer" className="border-chrome-border shrink-0 border-t py-3">
      <DataTablePagination className={className} {...props} />
    </div>
  );
}

export { DataPage, DataPanel, DataPanelBody, DataPanelPagination };
