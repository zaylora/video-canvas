import { useEffect, useState } from "react";

import { ChromePill, ChromeSeparator } from "@/components/canvas/chrome/chrome";
import { SITE_HEADER_CENTER_ID } from "@/components/home/site-header-center";
import { ThemeToggle } from "@/components/theme-toggle";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { CreditsMenu } from "@/pages/canvas/chrome/top-right-bar";

/**
 * 首页与所有画布页的顶栏：吸顶、半透明，滚动后多一条底边。
 * 左边是侧栏开关（桌面端收起 / 展开，窄屏打开抽屉，和后台顶栏一致），
 * 右边是一颗半透明胶囊：主题切换 | 积分；账号相关的都在侧栏底部的账号菜单里。
 * 正中留一个挂载点（SITE_HEADER_CENTER_ID），给各页面放收起后的工具条。
 */
export function SiteHeader() {
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 4);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header
      data-slot="site-header"
      className={cn(
        "bg-background/80 sticky top-0 z-20 flex h-14 shrink-0 items-center gap-2 border-b border-transparent px-4 backdrop-blur-xl transition-colors duration-150 md:rounded-t-[22px] md:px-5",
        scrolled && "border-border",
      )}
    >
      <SidebarTrigger className="size-8 rounded-full" />
      <div
        id={SITE_HEADER_CENTER_ID}
        className="absolute top-1/2 left-1/2 flex -translate-x-1/2 -translate-y-1/2 justify-center"
      />
      <div className="flex-1" />
      <ChromePill>
        <ThemeToggle />
        <ChromeSeparator />
        <CreditsMenu />
      </ChromePill>
    </header>
  );
}
