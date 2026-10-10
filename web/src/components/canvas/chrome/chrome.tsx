import type { ComponentProps, ReactNode } from "react";
import { LayoutGroup, motion, type HTMLMotionProps } from "motion/react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 画布外壳的积木（设计稿 docs/design/画布UI设计 6.1）：
 * 四角 + 左侧居中五个区，每个区里摆半透明胶囊，胶囊里是图标按钮、分隔线、分段控件。
 * 只管长相，摆什么按钮由页面组合。
 */

const ZONE_CLASS = {
  "top-left": "top-3 left-3",
  "top-right": "top-3 right-3",
  "bottom-left": "bottom-3.5 left-3",
  "bottom-right": "bottom-3.5 right-3",
  "left-center": "top-1/2 left-3 -translate-y-1/2",
} as const;

/** 外壳的一个区：贴在画布的某个角上。canvas-overlay-interactive 让它在抓手模式下照样能点 */
export function ChromeZone({
  position,
  className,
  ...props
}: ComponentProps<"div"> & { position: keyof typeof ZONE_CLASS }) {
  return (
    <div
      data-slot="chrome-zone"
      data-position={position}
      className={cn(
        "canvas-overlay-interactive pointer-events-auto absolute z-10 flex items-center gap-2",
        ZONE_CLASS[position],
        className,
      )}
      {...props}
    />
  );
}

/** 半透明胶囊：外壳上所有控件的底。vertical 是竖着排的，贴在左侧边用 */
export function ChromePill({
  size = "default",
  orientation = "horizontal",
  className,
  ...props
}: ComponentProps<"div"> & { size?: "default" | "lg"; orientation?: "horizontal" | "vertical" }) {
  const vertical = orientation === "vertical";
  return (
    <div
      data-slot="chrome-pill"
      className={cn(
        "bg-chrome text-foreground flex items-center gap-0.5 rounded-full shadow-lg ring-1 ring-chrome-border backdrop-blur-xl backdrop-saturate-150",
        vertical
          ? size === "lg"
            ? "w-13 flex-col gap-1 py-1.5"
            : "w-10 flex-col py-1"
          : size === "lg"
            ? "h-13 gap-1 px-1.5"
            : "h-10 px-1",
        className,
      )}
      {...props}
    />
  );
}

/** 胶囊里的分隔线：横排的胶囊里是竖线，竖排的胶囊里是横线 */
export function ChromeSeparator({
  orientation = "vertical",
  className,
  ...props
}: ComponentProps<"div"> & { orientation?: "horizontal" | "vertical" }) {
  return (
    <div
      role="separator"
      aria-orientation={orientation}
      className={cn(
        "bg-chrome-border shrink-0",
        orientation === "vertical" ? "mx-1 h-4.5 w-px" : "my-1 h-px w-4.5",
        className,
      )}
      {...props}
    />
  );
}

type ChromeButtonProps = HTMLMotionProps<"button"> & {
  /** primary 是白色实心主按钮（底部的「添加」） */
  variant?: "ghost" | "primary";
  size?: "default" | "lg";
  /** 当前选中（分段控件里用） */
  active?: boolean;
};

/** 胶囊里的图标按钮，按下有回弹 */
export function ChromeButton({
  variant = "ghost",
  size = "default",
  active,
  className,
  type = "button",
  ...props
}: ChromeButtonProps) {
  return (
    <motion.button
      type={type}
      data-slot="chrome-button"
      data-active={active || undefined}
      whileTap={props.disabled ? undefined : TAP}
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center gap-1.5 rounded-full whitespace-nowrap outline-none select-none",
        "focus-visible:ring-node-ring/60 transition-[background-color,color,opacity] duration-150 focus-visible:ring-2",
        "disabled:pointer-events-none disabled:opacity-35 [&_svg]:size-4 [&_svg]:shrink-0",
        size === "lg" ? "h-10 min-w-10 px-2.5" : "h-8 min-w-8 px-2",
        variant === "primary"
          ? "bg-foreground text-background hover:bg-foreground/90 w-10 px-0 [&_svg]:size-5 [&_svg]:stroke-[2.25]"
          : "text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-active:text-foreground",
        className,
      )}
      {...props}
    />
  );
}

/** 键位提示：tooltip 和菜单里用 */
export function Kbd({ className, ...props }: ComponentProps<"kbd">) {
  return (
    <kbd
      data-slot="kbd"
      className={cn("font-mono text-[11px] tracking-wide opacity-60", className)}
      {...props}
    />
  );
}

/** 给按钮套一个带快捷键的提示 */
export function ChromeTooltip({
  label,
  shortcut,
  side = "top",
  children,
}: {
  label: ReactNode;
  shortcut?: string;
  side?: "top" | "bottom" | "left" | "right";
  children: React.ReactElement;
}) {
  return (
    <Tooltip>
      <TooltipTrigger render={children} />
      <TooltipContent side={side} sideOffset={8}>
        {label}
        {shortcut && <Kbd>{shortcut}</Kbd>}
      </TooltipContent>
    </Tooltip>
  );
}

export type ChromeSegmentItem<T extends string> = {
  value: T;
  icon: ReactNode;
  label: string;
  shortcut?: string;
};

/**
 * 分段控件：选中块是一个 layoutId 滑块，在选项之间弹簧滑动。
 * id 要在页面里唯一，两组分段控件的滑块才不会互相飞过去。
 */
export function ChromeSegment<T extends string>({
  id,
  value,
  onValueChange,
  items,
  size = "default",
  orientation = "horizontal",
  tooltipSide = "top",
}: {
  id: string;
  value: T;
  onValueChange: (value: T) => void;
  items: ChromeSegmentItem<T>[];
  size?: "default" | "lg";
  orientation?: "horizontal" | "vertical";
  tooltipSide?: "top" | "bottom" | "left" | "right";
}) {
  return (
    <LayoutGroup id={id}>
      <div
        role="radiogroup"
        className={cn("flex items-center gap-0.5", orientation === "vertical" && "flex-col")}
      >
        {items.map((item) => {
          const active = item.value === value;
          return (
            <ChromeTooltip
              key={item.value}
              label={item.label}
              shortcut={item.shortcut}
              side={tooltipSide}
            >
              <ChromeButton
                role="radio"
                aria-checked={active}
                aria-label={item.label}
                size={size}
                active={active}
                onClick={() => onValueChange(item.value)}
              >
                {active && (
                  <motion.span
                    layoutId="segment-indicator"
                    transition={SPRING}
                    className="bg-chrome-hover absolute inset-0 rounded-full ring-1 ring-chrome-border"
                  />
                )}
                <span className="relative">{item.icon}</span>
              </ChromeButton>
            </ChromeTooltip>
          );
        })}
      </div>
    </LayoutGroup>
  );
}
