import type { ReactNode } from "react";
import { Link } from "react-router";

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
}: {
  to: string;
  title: string;
  active: boolean;
  thumb: ReactNode;
  trailing?: ReactNode;
}) {
  const { setOpenMobile } = useSidebar();
  return (
    <div className="group/item relative">
      <Link
        to={to}
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
