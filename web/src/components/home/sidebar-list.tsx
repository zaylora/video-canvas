import type { ReactNode } from "react";
import { Link } from "react-router";

import { Skeleton } from "@/components/ui/skeleton";
import { useSidebar } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";

/** 分组：一行小标题（右侧可放「全部」「新对话」之类的入口）+ 下面的列表 */
export function ListSection({
  title,
  action,
  children,
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="mt-2">
      <div className="flex h-8 items-center justify-between px-2.5">
        <p className="text-muted-foreground text-xs">{title}</p>
        {action}
      </div>
      <div className="grid gap-0.5">{children}</div>
    </section>
  );
}

/**
 * 分组里的一项：24px 缩略图（封面或图标）+ 标题，当前项高亮。
 * trailing 是右侧的附加内容（进行中的圆点、更多菜单）；它们在链接外面，点击不会触发跳转。
 */
export function ListItem({
  to,
  title,
  active,
  thumb,
  trailing,
  newWindow,
}: {
  to: string;
  title: string;
  active: boolean;
  thumb: ReactNode;
  trailing?: ReactNode;
  /** 在新窗口打开（画布） */
  newWindow?: boolean;
}) {
  const { setOpenMobile } = useSidebar();
  return (
    <div className="group/item relative">
      <Link
        to={to}
        {...(newWindow ? { target: "_blank", rel: "noopener" } : {})}
        title={title}
        aria-current={active ? "page" : undefined}
        onClick={() => setOpenMobile(false)}
        className={cn(
          "text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 flex h-9 items-center gap-2.5 rounded-[10px] pr-2 pl-2.5 text-sm outline-none focus-visible:ring-3",
          trailing && "pr-9",
          active && "bg-muted text-foreground font-medium",
        )}
      >
        <span className="bg-muted grid size-6 shrink-0 place-items-center overflow-hidden rounded-[7px] [&_svg]:size-3.5">
          {thumb}
        </span>
        <span className="min-w-0 flex-1 truncate">{title}</span>
      </Link>
      {trailing && <div className="absolute top-1/2 right-1.5 -translate-y-1/2">{trailing}</div>}
    </div>
  );
}

/** 骨架屏标题条的宽度，错开一点，看起来像真实的长短不一的标题 */
const SKELETON_TITLE_WIDTHS = ["70%", "55%", "80%", "60%"];

/**
 * 分组加载中的占位：每行的高度、缩略图和间距都和 ListItem 一致，数据到了直接替换，不会跳动。
 * @param rows 占几行
 */
export function ListSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div aria-busy className="grid gap-0.5">
      {Array.from({ length: rows }, (_, index) => (
        <div
          key={index}
          data-slot="list-skeleton-row"
          className="flex h-9 items-center gap-2.5 pr-2 pl-2.5"
        >
          <Skeleton className="size-6 shrink-0 rounded-[7px] motion-reduce:animate-none" />
          <Skeleton
            className="h-3.5 motion-reduce:animate-none"
            style={{
              width: SKELETON_TITLE_WIDTHS[index % SKELETON_TITLE_WIDTHS.length],
            }}
          />
        </div>
      ))}
    </div>
  );
}
