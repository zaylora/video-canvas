import * as React from "react";
import { Popover as PopoverPrimitive } from "@base-ui/react/popover";
import { cn } from "cn";

import { DURATION, EASE_OUT_CSS, ms } from "@/lib/motion";

/**
 * 后台专用气泡层：和 components/ui/popover 同一套 API，动效单独维护（画布不受影响）。
 * 支持 anchor（把气泡锚到触发器之外的元素，比如表格单元格）。
 */

/** 进入、退出的时长与曲线来自 lib/motion，用 CSS 变量交给 Tailwind；退出比进入快 */
const MOTION_VARS = {
  "--motion-in": ms(DURATION.base),
  "--motion-out": ms(DURATION.exit),
  "--motion-ease": EASE_OUT_CSS,
} as React.CSSProperties;

/** 合并动效变量与调用方 style；style 为函数时先求值再合并，不丢弃 */
function mergeStyle<S>(
  style: React.CSSProperties | ((state: S) => React.CSSProperties | undefined) | undefined,
) {
  return (state: S): React.CSSProperties => ({
    ...MOTION_VARS,
    ...(typeof style === "function" ? style(state) : style),
  });
}

function Popover({ ...props }: PopoverPrimitive.Root.Props) {
  return <PopoverPrimitive.Root data-slot="popover" {...props} />;
}

function PopoverTrigger({ ...props }: PopoverPrimitive.Trigger.Props) {
  return <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />;
}

function PopoverContent({
  className,
  align = "center",
  alignOffset = 0,
  side = "bottom",
  sideOffset = 4,
  anchor,
  style,
  ...props
}: PopoverPrimitive.Popup.Props &
  Pick<
    PopoverPrimitive.Positioner.Props,
    "align" | "alignOffset" | "side" | "sideOffset" | "anchor"
  >) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Positioner
        align={align}
        alignOffset={alignOffset}
        side={side}
        sideOffset={sideOffset}
        anchor={anchor}
        className="isolate z-50"
      >
        <PopoverPrimitive.Popup
          data-slot="popover-content"
          className={cn(
            "z-50 flex w-72 origin-(--transform-origin) flex-col gap-2.5 rounded-lg bg-popover p-2.5 text-sm text-popover-foreground shadow-md ring-1 ring-foreground/10 outline-hidden duration-(--motion-in) ease-(--motion-ease) data-open:animate-in data-open:fade-in-0 data-open:zoom-in-[0.96] data-closed:duration-(--motion-out) data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-[0.96] motion-reduce:data-open:zoom-in-100 motion-reduce:data-closed:zoom-out-100",
            className,
          )}
          style={mergeStyle(style)}
          {...props}
        />
      </PopoverPrimitive.Positioner>
    </PopoverPrimitive.Portal>
  );
}

function PopoverHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="popover-header"
      className={cn("flex flex-col gap-0.5 text-sm", className)}
      {...props}
    />
  );
}

function PopoverTitle({ className, ...props }: PopoverPrimitive.Title.Props) {
  return (
    <PopoverPrimitive.Title
      data-slot="popover-title"
      className={cn("font-medium", className)}
      {...props}
    />
  );
}

function PopoverDescription({ className, ...props }: PopoverPrimitive.Description.Props) {
  return (
    <PopoverPrimitive.Description
      data-slot="popover-description"
      className={cn("text-muted-foreground", className)}
      {...props}
    />
  );
}

export { Popover, PopoverContent, PopoverDescription, PopoverHeader, PopoverTitle, PopoverTrigger };
