import { useState, type CSSProperties } from "react";
import { Outlet, useLocation, useViewTransitionState } from "react-router";
import { motion, MotionConfig } from "motion/react";

import { AppSidebar } from "@/components/home/app-sidebar";
import { SiteHeader } from "@/components/home/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { usePageTitle } from "@/hooks/use-page-title";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { homePageTitle } from "@/utils/page-title";

/** 侧栏展开状态存在本机；和后台的侧栏分开记，互不影响 */
const SIDEBAR_KEY = "home-sidebar-open";

function readSidebarOpen() {
  try {
    return localStorage.getItem(SIDEBAR_KEY) !== "false";
  } catch {
    return true;
  }
}

/**
 * 内容区的页面入场：切到另一个一级页面（创作 / 资产 / 画布列表 / 个人资料…）时，
 * 新内容淡入并从下方 8px 上浮到位，侧栏和顶栏不动。
 * 以第一段路径作 key：资产页换范围、对话之间切换只改查询参数或 id，不会重播。
 * 画布列表自己有 view transition（画布卡缩放进出），不再叠这层，免得两套转场打架。
 * 系统开「减少动态效果」时 MotionConfig 会去掉位移，只剩淡入。
 */
function PageTransition() {
  const section = useLocation().pathname.split("/")[1] ?? "";
  return (
    <motion.div
      key={section}
      initial={section === "canvases" ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: DURATION.slow, ease: EASE_OUT }}
      className="flex min-h-0 flex-1 flex-col"
    >
      <Outlet />
    </motion.div>
  );
}

/**
 * 首页与所有画布页的外壳（设计稿 docs/design/首页）：左侧侧栏是底层，右边内容区是浮起的一张圆角页面（吸顶顶栏 + 页面内容）。
 * MotionConfig 的 reducedMotion="user" 让系统开了「减少动态效果」时只保留淡入淡出。
 * 首页点「更多」进所有画布页有 view transition：这个外壳不换，转场期间给侧栏和顶栏打上标记
 * （index.css 里据此给它们单独的 view-transition-name），让它们留在原地不跟着页面缩放。
 */
export default function HomeLayout() {
  const [open, setOpen] = useState(readSidebarOpen);
  const transitioning = useViewTransitionState("/canvases");
  usePageTitle(homePageTitle(useLocation().pathname));

  return (
    <MotionConfig reducedMotion="user">
      <SidebarProvider
        data-shell="home"
        className="home-theme"
        data-view-transitioning={transitioning || undefined}
        /** 设计稿 4.0：展开 240px，收起 56px（40px 图标按钮 + 两侧各 8px），shadcn 默认的 48px 放不下 */
        style={{ "--sidebar-width": "15rem", "--sidebar-width-icon": "3.5rem" } as CSSProperties}
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          try {
            localStorage.setItem(SIDEBAR_KEY, String(next));
          } catch {
            /** 隐私模式等存不进去时，只是下次不记得，不影响使用 */
          }
        }}
      >
        <AppSidebar />
        <SidebarInset className="ring-border min-w-0 md:peer-data-[variant=inset]:rounded-[22px] md:peer-data-[variant=inset]:shadow-[0_12px_40px_-18px_oklch(0_0_0/0.35)] md:peer-data-[variant=inset]:ring-1">
          <SiteHeader />
          <PageTransition />
        </SidebarInset>
      </SidebarProvider>
    </MotionConfig>
  );
}
