import { useEffect, useState } from "react";

import { ThemeToggle } from "@/components/home/theme-toggle";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { CreditsPill } from "@/pages/canvas/chrome/top-right-bar";

/**
 * 首页与所有画布页的顶栏：吸顶、半透明，滚动后多一条底边。
 * 左边是侧栏开关（桌面端收起 / 展开，窄屏打开抽屉，和后台顶栏一致），右边是主题切换和积分；账号相关的都在侧栏底部的账号菜单里。
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
        "bg-background/80 sticky top-0 z-20 flex h-14 shrink-0 items-center gap-2 border-b border-transparent px-4 backdrop-blur-xl transition-colors duration-150 md:px-6",
        scrolled && "border-border",
      )}
    >
      <SidebarTrigger className="size-8" />
      <div className="flex-1" />
      <ThemeToggle />
      <CreditsPill />
    </header>
  );
}
