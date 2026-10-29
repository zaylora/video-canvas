import { motion } from "motion/react";
import { Link } from "react-router";

import { Logo } from "@/components/brand/logo";
import { SPRING } from "@/lib/motion";

/**
 * 侧栏菜单按钮：40px 高的胶囊，收起后是 40px 的圆形图标按钮。首页侧栏和后台侧栏共用，保证两边大小和风格一致。
 * overflow-visible：选中块用 layoutId 在项之间滑动，被按钮自带的 overflow-hidden 裁掉就成了“跳”过去
 */
export const NAV_ITEM_CLASS =
  "h-10 gap-3 overflow-visible rounded-full pr-3 pl-3.5 whitespace-nowrap text-muted-foreground transition-colors duration-150 hover:bg-chrome-hover hover:text-foreground data-active:bg-transparent data-active:font-semibold data-active:text-foreground group-data-[collapsible=icon]:size-10! group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0! group-data-[collapsible=icon]:[&>span:last-child]:hidden [&_svg]:size-[18px]!";

/** 可点的导航项在 NAV_ITEM_CLASS 之外还要加上：给选中块定位，并让它垫在文字下面 */
export const NAV_LINK_CLASS = `${NAV_ITEM_CLASS} relative isolate`;

/**
 * 当前页的选中胶囊（半透明底 + 细描边，同 coding-standards 6.1 的选中态），用 layoutId 在菜单项之间滑动
 * @param layoutId 同一个侧栏里的选中块共用一个 id，不同侧栏各用各的
 */
export function NavActiveHighlight({ layoutId }: { layoutId: string }) {
  return (
    <motion.span
      layoutId={layoutId}
      transition={SPRING}
      className="bg-chrome-hover ring-chrome-border absolute inset-0 -z-10 rounded-full ring-1"
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
      className="focus-visible:ring-ring/50 flex min-w-0 items-center gap-2.5 rounded-md text-lg font-semibold tracking-wide outline-none focus-visible:ring-3"
    >
      <Logo size={30} />
      <span className="font-display truncate font-black tracking-wide group-data-[collapsible=icon]:hidden">
        连镜
      </span>
    </Link>
  );
}
