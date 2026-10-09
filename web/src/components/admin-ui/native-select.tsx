import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/** 与 Input 同一套外观的原生下拉：键盘、读屏都由浏览器负责 */
function NativeSelect({ className, ...props }: ComponentProps<"select">) {
  return (
    <select
      data-slot="native-select"
      className={cn(
        "border-input h-9 w-full min-w-0 appearance-none rounded-lg border bg-transparent py-0 pr-8 pl-3 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive dark:bg-input/30",
        className,
      )}
      {...props}
    />
  );
}

export { NativeSelect };
