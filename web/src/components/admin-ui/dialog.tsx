import * as React from "react";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { cn } from "cn";

import { DURATION, EASE_OUT_CSS, ms } from "@/lib/motion";

/**
 * 后台专用对话框：写法照 admin-ui/sheet，和 components/ui/dialog 同一套 API，动效单独维护（画布不受影响）。
 * 进入用 slow 档（opacity + translateY 8px→0 + scale 0.98→1），退出用 slowExit 档且位移更小；
 * 减少动态效果时去掉位移和缩放，只留淡入淡出。尺寸由使用方用 className 给，打开后尺寸不变，不做尺寸动画。
 */

/** 进入、退出的时长与曲线都来自 lib/motion，用 CSS 变量交给 Tailwind 的 duration-(--x) */
const MOTION_VARS = {
  "--motion-in": ms(DURATION.slow),
  "--motion-out": ms(DURATION.slowExit),
  "--motion-ease": EASE_OUT_CSS,
} as React.CSSProperties;

function Dialog({ ...props }: DialogPrimitive.Root.Props) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />;
}

function DialogTrigger({ ...props }: DialogPrimitive.Trigger.Props) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />;
}

function DialogClose({ ...props }: DialogPrimitive.Close.Props) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />;
}

function DialogPortal({ ...props }: DialogPrimitive.Portal.Props) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />;
}

function DialogOverlay({ className, ...props }: DialogPrimitive.Backdrop.Props) {
  return (
    <DialogPrimitive.Backdrop
      data-slot="dialog-overlay"
      className={cn(
        "fixed inset-0 z-50 bg-black/10 transition-opacity duration-(--motion-in) ease-(--motion-ease) data-ending-style:duration-(--motion-out) data-ending-style:opacity-0 data-starting-style:opacity-0 supports-backdrop-filter:backdrop-blur-xs",
        className,
      )}
      style={MOTION_VARS}
      {...props}
    />
  );
}

/**
 * 对话框内容：外层铺满视口做居中（不拦截点击，点遮罩仍能关闭），里面的 Popup 才是对话框本体。
 * 居中不用 translate，避免和进出场的位移打架。
 */
function DialogContent({
  className,
  style,
  ...props
}: DialogPrimitive.Popup.Props & React.RefAttributes<HTMLDivElement>) {
  return (
    <DialogPortal>
      <DialogOverlay />
      <div
        data-slot="dialog-positioner"
        className="pointer-events-none fixed inset-0 z-50 grid place-items-center p-4 max-md:p-0"
      >
        <DialogPrimitive.Popup
          data-slot="dialog-content"
          className={cn(
            "bg-popover text-popover-foreground ring-foreground/10 pointer-events-auto flex flex-col overflow-hidden rounded-xl text-sm shadow-xl ring-1 outline-none",
            "transition duration-(--motion-in) ease-(--motion-ease) data-ending-style:duration-(--motion-out)",
            "data-starting-style:translate-y-2 data-starting-style:scale-[0.98] data-starting-style:opacity-0",
            "data-ending-style:translate-y-1.5 data-ending-style:scale-[0.98] data-ending-style:opacity-0",
            "motion-reduce:data-ending-style:translate-y-0 motion-reduce:data-ending-style:scale-100 motion-reduce:data-starting-style:translate-y-0 motion-reduce:data-starting-style:scale-100",
            className,
          )}
          style={(state) => ({
            ...MOTION_VARS,
            ...(typeof style === "function" ? style(state) : style),
          })}
          {...props}
        />
      </div>
    </DialogPortal>
  );
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex h-14 shrink-0 items-center gap-4 border-b px-5", className)}
      {...props}
    />
  );
}

function DialogFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn("flex h-15 shrink-0 items-center gap-2 border-t px-5", className)}
      {...props}
    />
  );
}

function DialogTitle({ className, ...props }: DialogPrimitive.Title.Props) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn("font-heading text-base font-medium", className)}
      {...props}
    />
  );
}

function DialogDescription({ className, ...props }: DialogPrimitive.Description.Props) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn("text-muted-foreground text-sm", className)}
      {...props}
    />
  );
}

export {
  Dialog,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
};
