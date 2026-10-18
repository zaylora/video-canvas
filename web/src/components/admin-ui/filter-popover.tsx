import { SlidersHorizontal } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { DURATION } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 「更多筛选」弹层：少数人才用的筛选收在一个按钮后面，按钮上用角标显示已选了几项。
 * <FilterPopover>
 *   <FilterPopoverTrigger count={2}>更多筛选</FilterPopoverTrigger>
 *   <FilterPopoverContent><FilterPopoverField label="类型">…</FilterPopoverField></FilterPopoverContent>
 * </FilterPopover>
 */
function FilterPopover({ ...props }: ComponentProps<typeof Popover>) {
  return <Popover data-slot="filter-popover" {...props} />;
}

/** 触发按钮：图标 + 文字 + 已选数量角标（0 不显示） */
function FilterPopoverTrigger({
  className,
  count = 0,
  children,
  ...props
}: Omit<ComponentProps<typeof Button>, "variant" | "size"> & { count?: number }) {
  return (
    <PopoverTrigger
      render={
        <Button variant="outline" size="sm" className={cn("gap-1.5", className)} {...props} />
      }
    >
      <SlidersHorizontal />
      {children}
      {count > 0 && (
        <span
          data-slot="filter-popover-badge"
          className="bg-primary text-primary-foreground grid min-w-4 place-items-center rounded-full px-1 text-[11px] leading-4 tabular-nums"
        >
          {count}
        </span>
      )}
    </PopoverTrigger>
  );
}

/** 弹层本体：出现动画从按钮方向展开（origin 指向按钮），时长取 DURATION.base */
function FilterPopoverContent({
  className,
  style,
  ...props
}: ComponentProps<typeof PopoverContent>) {
  return (
    <PopoverContent
      data-slot="filter-popover-content"
      align="start"
      className={cn("w-72 gap-3 p-3", className)}
      style={{ animationDuration: `${DURATION.base}s`, ...style }}
      {...props}
    />
  );
}

/** 弹层里的一项：小标题在上，控件在下 */
function FilterPopoverField({
  label,
  className,
  children,
  ...props
}: Omit<ComponentProps<"div">, "children"> & { label: ReactNode; children: ReactNode }) {
  return (
    <div
      data-slot="filter-popover-field"
      className={cn("flex flex-col gap-1.5", className)}
      {...props}
    >
      <span className="text-muted-foreground text-xs">{label}</span>
      {children}
    </div>
  );
}

export { FilterPopover, FilterPopoverContent, FilterPopoverField, FilterPopoverTrigger };
