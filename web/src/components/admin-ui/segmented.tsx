import { motion } from "motion/react";
import type { ComponentProps } from "react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 分段选择：清透胶囊里一排按钮，选中的浮起一块同画布 ChromeSegment 的滑块。
 * <Segmented aria-label="能力"><SegmentedItem active>全部</SegmentedItem>…</Segmented>
 * 给每个 SegmentedItem 传同一个 slideId，选中的浮块就会用 layoutId + SPRING 滑到新位置；
 * 不传则选中项直接换底色（其他页面的旧用法不变）。同一页多组分段要用不同的 slideId。
 */
function Segmented({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      role="group"
      data-slot="segmented"
      className={cn(
        "bg-chrome ring-chrome-border inline-flex gap-0.5 rounded-full p-0.5 text-xs ring-1 backdrop-blur-xl",
        className,
      )}
      {...props}
    />
  );
}

function SegmentedItem({
  className,
  active,
  slideId,
  children,
  ...props
}: ComponentProps<"button"> & {
  active?: boolean;
  /** 滑块的 layoutId：同一组分段共用一个 */
  slideId?: string;
}) {
  return (
    <button
      type="button"
      aria-pressed={!!active}
      data-slot="segmented-item"
      data-active={active || undefined}
      className={cn(
        "text-muted-foreground hover:text-foreground rounded-full px-3 py-1.5 font-medium",
        slideId
          ? "relative data-active:text-foreground"
          : "data-active:bg-chrome-hover data-active:ring-chrome-border data-active:text-foreground data-active:ring-1",
        "disabled:cursor-not-allowed disabled:opacity-40",
        className,
      )}
      {...props}
    >
      {slideId && active && (
        <motion.span
          layoutId={slideId}
          transition={SPRING}
          className="bg-chrome-hover ring-chrome-border absolute inset-0 rounded-full ring-1"
        />
      )}
      {slideId ? <span className="relative">{children}</span> : children}
    </button>
  );
}

export { Segmented, SegmentedItem };
