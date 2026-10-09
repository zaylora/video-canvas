import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 下划线页签（模型 / 渠道 / 插件弹窗共用）：
 * <UnderlineTabs><UnderlineTab selected onClick>…</UnderlineTab></UnderlineTabs>
 * 只负责页签条；内容区由页面按选中项自己渲染（要保持挂载的用 hidden 隐藏）。
 */
function UnderlineTabs({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="underline-tabs"
      role="tablist"
      className={cn("flex items-center gap-1 border-b px-4", className)}
      {...props}
    />
  );
}

function UnderlineTab({
  className,
  selected,
  ...props
}: ComponentProps<"button"> & { selected: boolean }) {
  return (
    <button
      data-slot="underline-tab"
      type="button"
      role="tab"
      aria-selected={selected}
      className={cn(
        "-mb-px inline-flex items-center gap-2 border-b-2 px-3 py-3 text-sm font-medium [&_svg:not([class*='size-'])]:size-4",
        selected
          ? "border-foreground text-foreground"
          : "text-muted-foreground hover:text-foreground border-transparent",
        className,
      )}
      {...props}
    />
  );
}

export { UnderlineTab, UnderlineTabs };
