import { useState } from "react";
import { motion, useReducedMotion } from "motion/react";
import { ChevronLeft, ChevronRight } from "lucide-react";

import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 运营位占位：还没有内容来源，先把结构搭出来 */
const SLIDES = ["运营 Banner 占位 1", "运营 Banner 占位 2", "运营 Banner 占位 3"];

/**
 * 首页的运营 Banner（占位）：只能手动切换（箭头、圆点、左右方向键），不自动轮播，
 * 动效规范不允许常驻的循环动画。减少动态效果时直接切换，不滑动。
 */
export function BannerCarousel() {
  const [index, setIndex] = useState(0);
  const reduced = useReducedMotion();
  const go = (next: number) => setIndex((next + SLIDES.length) % SLIDES.length);

  return (
    <section
      aria-roledescription="carousel"
      aria-label="活动"
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft") go(index - 1);
        if (event.key === "ArrowRight") go(index + 1);
      }}
      className="group/banner bg-muted focus-visible:ring-ring/50 relative aspect-[16/7] overflow-hidden rounded-3xl outline-none focus-visible:ring-3 md:aspect-[21/5]"
    >
      <motion.div
        className="flex h-full"
        animate={{ x: `${-index * 100}%` }}
        transition={reduced ? { duration: 0 } : { duration: DURATION.slow, ease: EASE_OUT }}
      >
        {SLIDES.map((title, i) => (
          <div
            key={title}
            aria-hidden={i !== index}
            className="flex h-full w-full shrink-0 items-end bg-[repeating-linear-gradient(135deg,transparent_0_14px,var(--chrome-hover)_14px_15px)] p-6 md:px-9 md:py-7"
          >
            <div>
              <span className="text-muted-foreground text-[13px]">活动位 · 即将上线</span>
              <b className="block text-xl font-semibold md:text-[26px]">{title}</b>
            </div>
          </div>
        ))}
      </motion.div>

      {(
        [
          ["上一张", -1, ChevronLeft, "left-4"],
          ["下一张", 1, ChevronRight, "right-4"],
        ] as const
      ).map(([label, step, Icon, side]) => (
        <motion.button
          key={label}
          type="button"
          aria-label={label}
          whileTap={TAP}
          onClick={() => go(index + step)}
          className={cn(
            "bg-chrome ring-chrome-border absolute top-1/2 grid size-10 -translate-y-1/2 place-items-center rounded-full ring-1 backdrop-blur-xl outline-none",
            "opacity-0 transition-opacity duration-150 group-hover/banner:opacity-100 focus-visible:opacity-100 max-md:hidden",
            side,
          )}
        >
          <Icon className="size-4" />
        </motion.button>
      ))}

      <div className="absolute bottom-3.5 left-1/2 flex -translate-x-1/2 gap-1.5">
        {SLIDES.map((title, i) => (
          <button
            key={title}
            type="button"
            aria-label={`第 ${i + 1} 张`}
            aria-current={i === index}
            onClick={() => go(i)}
            className={cn(
              "size-1.5 rounded-full transition-colors duration-150 outline-none focus-visible:ring-2 focus-visible:ring-ring",
              i === index ? "bg-foreground" : "bg-foreground/30",
            )}
          />
        ))}
      </div>
    </section>
  );
}
