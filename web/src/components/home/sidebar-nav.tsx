import { motion } from "motion/react";
import { Link } from "react-router";

import { Logo } from "@/components/brand/logo";
import { SPRING } from "@/lib/motion";

/**
 * 侧栏菜单按钮统一高度 40px，收起后是 40px 见方的图标按钮。首页侧栏和后台侧栏共用，保证两边大小一致。
 * overflow-visible：选中块用 layoutId 在项之间滑动，被按钮自带的 overflow-hidden 裁掉就成了“跳”过去
 */
export const NAV_ITEM_CLASS =
  "h-10 gap-3 overflow-visible rounded-[10px] px-2.5 text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-active:bg-transparent data-active:font-semibold data-active:text-foreground group-data-[collapsible=icon]:size-10! group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0! group-data-[collapsible=icon]:[&>span:last-child]:hidden [&_svg]:size-[18px]!";

/** 可点的导航项在 NAV_ITEM_CLASS 之外还要加上：给选中块定位，并让它垫在文字下面 */
export const NAV_LINK_CLASS = `${NAV_ITEM_CLASS} relative isolate`;

/**
 * 当前页的灰块 + 左侧竖条，用 layoutId 在菜单项之间滑动
 * @param layoutId 同一个侧栏里的选中块共用一个 id，不同侧栏各用各的
 */
export function NavActiveHighlight({ layoutId }: { layoutId: string }) {
  return (
    <motion.span
      layoutId={layoutId}
      transition={SPRING}
      className="bg-muted before:bg-foreground absolute inset-0 -z-10 rounded-[10px] before:absolute before:top-2.5 before:bottom-2.5 before:-left-2 before:w-[3px] before:rounded-r-full"
    />
  );
}

/**
 * 侧栏顶部的「Logo + 连镜」链接，首页侧栏和后台侧栏共用；侧栏收起后只剩 Logo
 * @param to 点击跳转的地址
 */
export function SidebarBrand({ to }: { to: string }) {
  return (
    <Link
      to={to}
      aria-label="连镜"
      className="focus-visible:ring-ring/50 flex min-w-0 items-center gap-2 rounded-md text-[15px] font-semibold tracking-wide outline-none focus-visible:ring-3"
    >
      <Logo size={24} />
      <span className="truncate group-data-[collapsible=icon]:hidden">连镜</span>
    </Link>
  );
}
