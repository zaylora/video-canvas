import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 竖向时间线（版本历史）：
 * <Timeline><TimelineItem><TimelineIndicator active><Icon /></TimelineIndicator><TimelineContent>…</TimelineContent></TimelineItem></Timeline>
 * 节点之间的连线由 TimelineItem 自己画，最后一项不画。
 */
function Timeline({ className, ...props }: ComponentProps<"ol">) {
  return <ol data-slot="timeline" className={cn("flex flex-col", className)} {...props} />;
}

function TimelineItem({ className, children, ...props }: ComponentProps<"li">) {
  return (
    <li
      data-slot="timeline-item"
      className={cn("group/timeline-item relative flex gap-4 pb-6 last:pb-0", className)}
      {...props}
    >
      <span
        aria-hidden="true"
        className="bg-border absolute top-7 bottom-0 left-[11px] w-px group-last/timeline-item:hidden"
      />
      {children}
    </li>
  );
}

/** 节点圆圈；active（如最新版本）高亮 */
function TimelineIndicator({
  className,
  active,
  ...props
}: ComponentProps<"span"> & { active?: boolean }) {
  return (
    <span
      data-slot="timeline-indicator"
      data-active={active || undefined}
      className={cn(
        "border-border bg-background text-muted-foreground relative z-10 mt-1 grid size-6 shrink-0 place-items-center rounded-full border-2 [&>svg]:size-3",
        "data-active:border-sky-500 data-active:bg-sky-500/15 data-active:text-sky-500",
        className,
      )}
      {...props}
    />
  );
}

function TimelineContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <div data-slot="timeline-content" className={cn("min-w-0 flex-1", className)} {...props} />
  );
}

function TimelineHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="timeline-header"
      className={cn("flex flex-wrap items-center gap-2", className)}
      {...props}
    />
  );
}

export { Timeline, TimelineContent, TimelineHeader, TimelineIndicator, TimelineItem };
