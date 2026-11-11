import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { motion, useReducedMotion } from "motion/react";
import { X } from "lucide-react";

import type { AnnouncementDto } from "@/api/announcement/type";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { MARQUEE_HOLD_MS, dwellMs, marqueePlan, type MarqueePlan } from "@/utils/home/announcement";

/** 当前这条公告还剩多久切走：key 区分是哪一条（含重播次数），换条后旧计时器的清理不会扣到新的一条上 */
type Remaining = { key: string; ms: number };

/**
 * 首页公告胶囊（设计稿 docs/design/公告/公告.md §13 v5）：和右上角 ChromePill 同一种清透材质，
 * 主题黄小圆点 + 一句话 + ×；只有一句话，没有详情、没有链接，每条都能关。
 * 只负责胶囊本身，放在哪里由调用方定（首页挂在顶栏正中悬浮，后台编辑抽屉里做预览）。
 * 一次一条：标题放得下停 5 秒；放不下先停 1.5 秒，再从右往左滚到末尾，停 1.5 秒后才切下一条。
 * 悬停、键盘焦点在条内、页面不可见时，滚动和计时一起停在原位，恢复后接着走。
 * 系统开「减少动态效果」时不滚动，放不下的标题显示省略号。
 * @param items 要显示的公告（已按结束时间和关闭记录过滤），为空时不渲染
 * @param onClose 用户关闭一条公告
 */
export function AnnouncementBar({
  items,
  onClose,
}: {
  items: AnnouncementDto[];
  onClose: (item: AnnouncementDto) => void;
}) {
  const reduced = useReducedMotion();
  const [currentId, setCurrentId] = useState<number | null>(null);
  /** 只有一条且要滚动时，用它触发「回到开头重新滚」 */
  const [cycle, setCycle] = useState(0);
  const [hover, setHover] = useState(false);
  const [focused, setFocused] = useState(false);
  const [pageHidden, setPageHidden] = useState(() => document.visibilityState === "hidden");

  const index = Math.max(
    0,
    items.findIndex((item) => item.id === currentId),
  );
  const current = items[index] as AnnouncementDto | undefined;
  const paused = hover || focused || pageHidden;
  const itemKey = current ? `${current.id}:${current.version}:${cycle}` : "";

  const rowRef = useRef<HTMLDivElement>(null);
  const viewportRef = useRef<HTMLSpanElement>(null);
  const titleRef = useRef<HTMLSpanElement>(null);
  const planRef = useRef<MarqueePlan | null>(null);
  const animRef = useRef<Animation | null>(null);
  const remainingRef = useRef<Remaining>({ key: "", ms: 0 });
  const pausedRef = useRef(paused);

  /** 让下面的测量 effect 拿到最新的暂停状态（同一次提交里先于它执行） */
  useLayoutEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  useEffect(() => {
    const onVisibility = () => setPageHidden(document.visibilityState === "hidden");
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, []);

  /** 每换一条（或重播）测一次标题是否放得下，放不下就排好滚动；窗口宽度变化在下一条生效 */
  useLayoutEffect(() => {
    animRef.current?.cancel();
    animRef.current = null;
    const row = rowRef.current;
    const viewport = viewportRef.current;
    const title = titleRef.current;
    if (!itemKey || !row || !viewport || !title) return;

    delete row.dataset.over;
    const plan = marqueePlan(title.scrollWidth - viewport.clientWidth);
    planRef.current = reduced ? null : plan;
    remainingRef.current = { key: itemKey, ms: dwellMs(planRef.current) };
    if (!plan) return;
    row.dataset.over = reduced ? "ellipsis" : "scroll";
    if (reduced) return;

    const { travel, runMs, totalMs } = plan;
    const shift = `translateX(${-travel}px)`;
    animRef.current = title.animate(
      [
        { transform: "translateX(0)", offset: 0 },
        { transform: "translateX(0)", offset: MARQUEE_HOLD_MS / totalMs },
        { transform: shift, offset: (MARQUEE_HOLD_MS + runMs) / totalMs },
        { transform: shift, offset: 1 },
      ],
      { duration: totalMs, easing: "linear", fill: "forwards" },
    );
    if (pausedRef.current) animRef.current.pause();
  }, [itemKey, reduced]);

  /** 暂停 / 恢复滚动 */
  useEffect(() => {
    const anim = animRef.current;
    if (!anim) return;
    if (paused) anim.pause();
    else if (anim.playState === "paused") anim.play();
  }, [paused, itemKey]);

  /** 停留计时：暂停时把剩余时间记下来，恢复后接着倒数；多条才轮换，只有一条且要滚时到点重播 */
  useEffect(() => {
    if (!itemKey || paused) return;
    if (items.length < 2 && !planRef.current) return;
    const key = itemKey;
    const startedAt = performance.now();
    const timer = window.setTimeout(() => {
      if (items.length > 1) setCurrentId(items[(index + 1) % items.length].id);
      else setCycle((value) => value + 1);
    }, remainingRef.current.ms);
    return () => {
      window.clearTimeout(timer);
      if (remainingRef.current.key === key) {
        remainingRef.current = {
          key,
          ms: Math.max(0, remainingRef.current.ms - (performance.now() - startedAt)),
        };
      }
    };
  }, [itemKey, paused, items, index]);

  if (!current) return null;

  const close = () => {
    const next = items[(index + 1) % items.length];
    setCurrentId(next && next.id !== current.id ? next.id : null);
    onClose(current);
  };

  return (
    <div
      ref={rowRef}
      role="region"
      aria-label="公告"
      aria-live="off"
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      onFocus={() => setFocused(true)}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false);
      }}
      className={cn(
        "announcement-row bg-chrome text-foreground ring-chrome-border inline-flex h-8 max-w-full min-w-0 items-center gap-2.5 rounded-full pl-3.5 text-[13px] shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150",
        "pr-1",
      )}
    >
      {/* 唯一的彩色：主题黄小圆点，外圈一层同色淡光 */}
      <span
        aria-hidden
        className="size-1.5 shrink-0 rounded-full bg-(--announcement-dot) shadow-[0_0_0_3px_color-mix(in_oklab,var(--announcement-dot)_18%,transparent)]"
      />
      <motion.div
        key={itemKey}
        initial={{ opacity: 0, y: 4 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: DURATION.fast, ease: EASE_OUT }}
        className="flex min-w-0 items-center"
      >
        <span
          ref={viewportRef}
          className="announcement-viewport min-w-0 overflow-hidden whitespace-nowrap"
        >
          <span
            ref={titleRef}
            title={current.title}
            className="announcement-title inline-block font-medium"
          >
            {current.title}
          </span>
        </span>
      </motion.div>
      {
        <>
          <span aria-hidden className="bg-chrome-border h-3.5 w-px shrink-0" />
          <button
            type="button"
            aria-label="关闭这条公告"
            onClick={close}
            className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 grid size-6.5 shrink-0 place-items-center rounded-full outline-none transition-colors duration-150 focus-visible:ring-2"
          >
            <X className="size-3.5" />
          </button>
        </>
      }
    </div>
  );
}
