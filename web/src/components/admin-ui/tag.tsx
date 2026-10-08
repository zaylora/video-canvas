import type { ComponentProps } from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

/** 语气配色（无描边的浅色底 + 同色文字，和原型一样轻），标签之外的卡片、图标块也用它上色；保留 border 宽度是为了不影响尺寸 */
const toneClasses = {
  neutral: "border-transparent bg-muted text-muted-foreground",
  success: "border-transparent bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  info: "border-transparent bg-sky-500/10 text-sky-700 dark:text-sky-400",
  warning: "border-transparent bg-amber-500/10 text-amber-700 dark:text-amber-400",
  danger: "border-transparent bg-red-500/10 text-red-700 dark:text-red-400",
  violet: "border-transparent bg-violet-500/10 text-violet-700 dark:text-violet-400",
  rose: "border-transparent bg-rose-500/10 text-rose-700 dark:text-rose-400",
  orange: "border-transparent bg-orange-500/10 text-orange-700 dark:text-orange-400",
} as const;

/** 设计稿的 badge */
const tagVariants = cva(
  "inline-flex w-fit shrink-0 items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs font-medium whitespace-nowrap [&>svg]:size-3 [&>svg]:shrink-0",
  {
    variants: { tone: toneClasses, mono: { true: "font-mono" } },
    defaultVariants: { tone: "neutral" },
  },
);

/** 小标签的语气 */
type TagTone = NonNullable<VariantProps<typeof tagVariants>["tone"]>;

/** 小标签：来源、状态、能力、版本等 */
function Tag({
  className,
  tone,
  mono,
  ...props
}: ComponentProps<"span"> & VariantProps<typeof tagVariants>) {
  return (
    <span
      data-slot="tag"
      data-tone={tone ?? "neutral"}
      className={cn(tagVariants({ tone, mono }), className)}
      {...props}
    />
  );
}

export { Tag, tagVariants, toneClasses, type TagTone };
