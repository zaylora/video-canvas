import type { CSSProperties, ReactNode } from "react";

import { cn } from "@/lib/utils";

/** 占位渐变用的色相来源：全是 index.css 的 token，深浅主题各自有定义 */
const TINTS = [
  "--beam",
  "--status-running",
  "--status-success",
  "--status-warning",
  "--preset",
  "--credit",
] as const;

const hash = (seed: string): number => {
  let h = 0;
  for (const ch of seed) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return h;
};

/** 按种子算出稳定的渐变：同一个名字永远是同一块色，用 token 调出来，不写死颜色 */
export function posterGradient(seed: string): string {
  const h = hash(seed);
  const tint = TINTS[h % TINTS.length];
  const angle = 140 + (h % 5) * 12;
  const amount = 28 + (h % 4) * 6;
  return `linear-gradient(${angle}deg, color-mix(in oklch, var(${tint}) ${amount}%, var(--muted)), color-mix(in oklch, var(${tint}) ${amount / 3}%, var(--muted)))`;
}

/**
 * 没有封面 / 形象时的渐变占位（一期后端不提供封面）。
 * @param seed 种子，一般是名字
 * @param children 叠在上面的内容
 */
export function DramaPoster({
  seed,
  className,
  children,
}: {
  seed: string;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <div
      data-slot="drama-poster"
      className={cn("relative overflow-hidden", className)}
      style={{ backgroundImage: posterGradient(seed) } as CSSProperties}
    >
      {children}
    </div>
  );
}
