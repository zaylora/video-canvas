import { useState, type CSSProperties } from "react";
import { Outlet, useViewTransitionState } from "react-router";
import { MotionConfig } from "motion/react";

import { AppSidebar } from "@/components/home/app-sidebar";
import { SiteHeader } from "@/components/home/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";

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
 * 首页与所有画布页的外壳（设计稿 docs/design/首页）：左侧侧栏 + 吸顶顶栏 + 页面内容。
 * MotionConfig 的 reducedMotion="user" 让系统开了「减少动态效果」时只保留淡入淡出。
 * 首页点「更多」进所有画布页有 view transition：这个外壳不换，转场期间给侧栏和顶栏打上标记
 * （index.css 里据此给它们单独的 view-transition-name），让它们留在原地不跟着页面缩放。
 */
export default function HomeLayout() {
  const [open, setOpen] = useState(readSidebarOpen);
  const transitioning = useViewTransitionState("/canvases");

  return (
    <MotionConfig reducedMotion="user">
      <SidebarProvider
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
        <SidebarInset className="min-w-0">
          <SiteHeader />
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    </MotionConfig>
  );
}
