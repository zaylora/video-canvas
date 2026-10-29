import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { motion, useReducedMotion } from "motion/react";

import { SITE_HEADER_CENTER_ID } from "@/components/home/site-header-center";
import { cn } from "@/lib/utils";
import { COLLAPSE_AT, collapseProgress } from "@/utils/assets/toolbar-collapse";

/** 吸顶顶栏的高度（SiteHeader 的 h-14），工具条滑到它下面才开始收 */
const HEADER_HEIGHT = 56;

/** 工具条和收起胶囊共用的半透明胶囊底（coding-standards 6.1） */
const CHROME =
  "bg-chrome ring-chrome-border flex items-center gap-1 rounded-full shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150";

/**
 * 资产页的工具条：一条半透明胶囊，放内容类型、筛选、搜索等。
 * 往下滑时工具条跟着滚动距离缩小、变淡（直接写 style，跟手不经过 React）；
 * 滑过顶栏后，同一份内容以小胶囊的样子挂到顶栏正中，从工具条原来的位置和宽度收拢过去（320ms 抽屉曲线）；
 * 滑回来时小胶囊直接撤掉，工具条复原。内容渲染两份，状态都在调用方，两边始终一致。
 * 窄屏顶栏放不下，不挂小胶囊；减少动态效果时不做收拢，只淡入。
 * @param children 工具条里的控件
 */
export function AssetsToolbar({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  const barRef = useRef<HTMLDivElement>(null);
  const miniRef = useRef<HTMLDivElement>(null);
  const [collapsed, setCollapsed] = useState(false);
  const reduced = useReducedMotion();

  useEffect(() => {
    const bar = barRef.current;
    if (!bar) return;
    const onScroll = () => {
      // 缩放原点在顶边，顶边位置不受缩放影响
      const top = bar.getBoundingClientRect().top + window.scrollY;
      const progress = collapseProgress({
        scrollY: window.scrollY,
        top,
        height: bar.offsetHeight,
        offset: HEADER_HEIGHT,
      });
      bar.style.transform = progress ? `scale(${1 - progress * 0.05})` : "";
      bar.style.opacity = progress ? String(1 - progress * 0.7) : "";
      setCollapsed(progress >= COLLAPSE_AT);
    };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
    };
  }, []);

  // 小胶囊出现时：从工具条的位置和宽度收拢到顶栏正中
  useLayoutEffect(() => {
    const bar = barRef.current;
    const mini = miniRef.current;
    if (!collapsed || reduced || !bar || !mini) return;
    const from = bar.getBoundingClientRect();
    const to = mini.getBoundingClientRect();
    mini.animate(
      [
        { width: `${from.width}px`, transform: `translateY(${from.top - to.top}px)`, opacity: 0.6 },
        { width: `${to.width}px`, transform: "none", opacity: 1 },
      ],
      { duration: 320, easing: "cubic-bezier(0.32, 0.72, 0, 1)" },
    );
  }, [collapsed, reduced]);

  // 收起时顶栏一定已经挂上了，渲染时直接取挂载点
  const host = collapsed ? document.getElementById(SITE_HEADER_CENTER_ID) : null;

  return (
    <>
      <div
        ref={barRef}
        data-slot="assets-toolbar"
        className={cn(CHROME, "relative z-10 flex-wrap p-1.5 will-change-transform", className)}
        style={{ transformOrigin: "50% 0" }}
      >
        {children}
      </div>
      {host &&
        createPortal(
          <div
            ref={miniRef}
            data-slot="assets-toolbar-mini"
            className={cn(
              CHROME,
              "h-11 max-w-[min(760px,calc(100vw-440px))] overflow-hidden p-1 max-md:hidden",
            )}
          >
            {/* 胶囊里控件不换行、不压扁；搜索框收窄，整条放得下 */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.2, delay: reduced ? 0 : 0.12 }}
              className="flex min-w-0 items-center gap-1 whitespace-nowrap *:shrink-0 [&_[data-slot=asset-search]]:w-40"
            >
              {children}
            </motion.div>
          </div>,
          host,
        )}
    </>
  );
}
