import { LayoutGroup, motion } from "motion/react";
import type { ReactNode } from "react";

import { ChromeButton, ChromePill } from "@/components/canvas/chrome/chrome";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 后台的「清透」面板外观（web/docs/coding-standards.md 6.1，与前台画布的胶囊同一套 token）：
 * 盖在 Card 等面板的 className 上，把实心卡片换成半透明 + 背景模糊 + 大圆角，底下透出后台外壳的彩色光晕。
 * 弹窗、下拉这类浮层不用它，仍是实色。
 */
export const GLASS_PANEL =
  "bg-chrome ring-chrome-border rounded-3xl shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150";

/**
 * 后台弹层（弹窗、确认框、抽屉）的清透外观：比面板更模糊、投影更深，压在遮罩上仍读得清。
 */
export const GLASS_POPUP =
  "bg-chrome ring-chrome-border shadow-2xl ring-1 backdrop-blur-2xl backdrop-saturate-150";

/** 弹层遮罩：压暗一点并轻微模糊，让清透弹层和底下的页面拉开层次 */
export const GLASS_OVERLAY = "bg-black/25 supports-backdrop-filter:backdrop-blur-sm";

/**
 * 后台外壳顶部的彩色光晕：清透面板要有东西可透，才不会变回一块灰底。
 * 铺在内容区（SidebarInset）里、所有内容之下，只是装饰，不响应指针，也不参与读屏。
 */
export function AdminGlow() {
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-x-0 top-0 -z-10 h-[640px] overflow-hidden"
    >
      <div className="bg-chart-1/25 dark:bg-chart-1/20 absolute -top-24 left-[8%] size-[420px] rounded-full blur-[120px]" />
      <div className="bg-chart-2/20 dark:bg-chart-2/18 absolute top-10 left-[42%] size-[380px] rounded-full blur-[120px]" />
      <div className="bg-status-success/15 dark:bg-status-success/12 absolute top-40 right-[4%] size-[340px] rounded-full blur-[120px]" />
    </div>
  );
}

/**
 * 清透胶囊里的文字分段：选中项浮起一块同 ChromeSegment 的滑块。
 * @param id 滑块的 layoutId 分组，同一页多组要不同
 * @param value 当前值
 * @param onValueChange 切换
 * @param items 选项
 * @param size sm 用在卡片头部，default 用在页头
 */
export function GlassSegmented<T extends string | number>({
  id,
  label,
  value,
  onValueChange,
  items,
  size = "default",
}: {
  id: string;
  label: string;
  value: T;
  onValueChange: (value: T) => void;
  items: { value: T; label: ReactNode }[];
  size?: "default" | "sm";
}) {
  return (
    <LayoutGroup id={id}>
      <ChromePill
        role="radiogroup"
        aria-label={label}
        className={cn(size === "sm" && "h-8 shadow-none")}
      >
        {items.map((item) => {
          const active = item.value === value;
          return (
            <ChromeButton
              key={item.value}
              role="radio"
              aria-checked={active}
              active={active}
              className={cn("text-xs font-medium", size === "sm" ? "h-6.5 px-2.5" : "px-3")}
              onClick={() => onValueChange(item.value)}
            >
              {active && (
                <motion.span
                  layoutId="glass-segment"
                  transition={SPRING}
                  className="bg-chrome-hover ring-chrome-border absolute inset-0 rounded-full ring-1"
                />
              )}
              <span className="relative">{item.label}</span>
            </ChromeButton>
          );
        })}
      </ChromePill>
    </LayoutGroup>
  );
}
