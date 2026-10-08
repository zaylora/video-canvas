import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 主从布局左侧的列表卡片（渠道、插件列表）：
 * <ListPanel>
 *   <ListPanelHeader><ListPanelTitle>全部渠道<ListPanelCount>3</ListPanelCount></ListPanelTitle>…</ListPanelHeader>
 *   <ListPanelContent><ListPanelItem active>…</ListPanelItem></ListPanelContent>
 * </ListPanel>
 */
function ListPanel({ className, ...props }: ComponentProps<"section">) {
  return (
    <section
      data-slot="list-panel"
      className={cn("bg-card flex min-h-0 flex-col rounded-xl border", className)}
      {...props}
    />
  );
}

function ListPanelHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="list-panel-header"
      className={cn("flex flex-col gap-3 border-b p-4", className)}
      {...props}
    />
  );
}

function ListPanelTitle({ className, children, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="list-panel-title"
      className={cn("flex items-center justify-between text-sm font-semibold", className)}
      {...props}
    >
      {children}
    </div>
  );
}

function ListPanelCount({ className, ...props }: ComponentProps<"span">) {
  return (
    <span
      data-slot="list-panel-count"
      className={cn(
        "bg-muted text-muted-foreground rounded-md px-1.5 font-mono text-xs font-normal",
        className,
      )}
      {...props}
    />
  );
}

function ListPanelContent({ className, ...props }: ComponentProps<"ul">) {
  return (
    <ul
      data-slot="list-panel-content"
      className={cn("min-h-0 flex-1 space-y-1 overflow-y-auto p-2", className)}
      {...props}
    />
  );
}

/** 列表项：整行是按钮；active 表示当前选中 */
function ListPanelItem({
  className,
  active,
  ...props
}: ComponentProps<"button"> & { active?: boolean }) {
  return (
    <li data-slot="list-panel-item">
      <button
        type="button"
        aria-current={active || undefined}
        data-active={active || undefined}
        className={cn(
          "w-full rounded-lg border border-transparent px-3 py-2.5 text-left transition",
          "hover:bg-accent/60 data-active:bg-accent",
          className,
        )}
        {...props}
      />
    </li>
  );
}

/** 列表项里的一行：左右分布，右侧用 ml-auto 的元素 */
function ListPanelItemRow({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="list-panel-item-row"
      className={cn("flex min-w-0 items-center gap-2", className)}
      {...props}
    />
  );
}

/** 空列表 / 加载失败等提示 */
function ListPanelEmpty({ className, ...props }: ComponentProps<"li">) {
  return (
    <li
      data-slot="list-panel-empty"
      className={cn("text-muted-foreground px-3 py-10 text-center text-sm", className)}
      {...props}
    />
  );
}

export {
  ListPanel,
  ListPanelContent,
  ListPanelCount,
  ListPanelEmpty,
  ListPanelHeader,
  ListPanelItem,
  ListPanelItemRow,
  ListPanelTitle,
};
