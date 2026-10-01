import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

const GRADIENTS = [
  "from-amber-400 to-orange-600",
  "from-sky-400 to-blue-600",
  "from-emerald-400 to-teal-600",
  "from-violet-400 to-purple-600",
  "from-rose-400 to-pink-600",
];

/** 用 seed 稳定地挑一个渐变色 */
const gradientOf = (seed: string) =>
  GRADIENTS[[...seed].reduce((sum, ch) => sum + ch.charCodeAt(0), 0) % GRADIENTS.length];

/** 名字的首字（去掉括号里的补充说明），没有名字显示 ? */
const initialOf = (name: string) =>
  name
    .replace(/[（(].*$/, "")
    .trim()
    .slice(0, 1)
    .toUpperCase() || "?";

/**
 * 没有 Logo 时的首字头像：渐变底色 + 名字首字。尺寸与字号用 className 调（默认 size-9 text-sm）。
 * @param seed 决定颜色的字符串（一般用 key），同一个对象颜色不变
 */
function InitialAvatar({
  name,
  seed,
  className,
  ...props
}: ComponentProps<"span"> & { name: string; seed?: string }) {
  return (
    <span
      data-slot="initial-avatar"
      aria-hidden="true"
      className={cn(
        "grid size-9 shrink-0 place-items-center rounded-lg bg-gradient-to-br text-sm font-semibold text-white shadow-sm",
        gradientOf(seed || name || "?"),
        className,
      )}
      {...props}
    >
      {initialOf(name)}
    </span>
  );
}

export { InitialAvatar };
