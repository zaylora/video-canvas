import NumberFlow from "@number-flow/react";
import { useReducedMotion } from "motion/react";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 会滚动的数字（@number-flow/react）：数值变大向上滚、变小向下滚，等宽数字。
 * 开了「减少动态效果」就不滚，直接换成新数字。
 */
function AnimatedNumber({
  className,
  value,
  ...props
}: Omit<ComponentProps<typeof NumberFlow>, "value" | "animated"> & { value: number }) {
  const reduced = useReducedMotion();
  return (
    <NumberFlow
      data-slot="animated-number"
      value={value}
      animated={!reduced}
      className={cn("tabular-nums", className)}
      {...props}
    />
  );
}

export { AnimatedNumber };
