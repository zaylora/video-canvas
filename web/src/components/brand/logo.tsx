import { useId, type SVGProps } from "react";

import { cn } from "@/lib/utils";

/** 小于等于这个尺寸（px）换成小尺寸版：括号加粗、光束改成实心短条，不做渐隐 */
const SMALL_MAX = 24;

/**
 * 「连镜」的 Logo「镜间光」：两个括号是取景框，中间一道琥珀色的光束。
 * 括号用 currentColor 跟随文字色；光束用品牌 token，深浅主题各取一档，
 * 压在视频上的场景传 bright 用永远偏亮的那一档。
 * @param size 边长（px），不超过 24 时自动换小尺寸版
 * @param bright 光束用 beam-bright：登录页舞台这类永远偏暗的底
 */
export function Logo({
  size = 32,
  bright = false,
  className,
  ...props
}: Omit<SVGProps<SVGSVGElement>, "viewBox" | "width" | "height"> & {
  size?: number;
  bright?: boolean;
}) {
  const uid = useId();
  const fadeId = `${uid}-fade`;
  const maskId = `${uid}-mask`;
  const beam = bright ? "var(--beam-bright)" : "var(--beam)";
  const small = size <= SMALL_MAX;

  return (
    <svg
      viewBox="0 0 48 48"
      width={size}
      height={size}
      aria-hidden="true"
      data-slot="logo"
      className={cn("shrink-0", className)}
      {...props}
    >
      {small ? (
        <>
          <path
            d="M18 7h-6a5 5 0 0 0-5 5v24a5 5 0 0 0 5 5h6"
            fill="none"
            stroke="currentColor"
            strokeWidth={5}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M30 7h6a5 5 0 0 1 5 5v24a5 5 0 0 1-5 5h-6"
            fill="none"
            stroke="currentColor"
            strokeWidth={5}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <rect x="20.5" y="12" width="7" height="24" rx="3.5" fill={beam} />
        </>
      ) : (
        <>
          <defs>
            <linearGradient id={fadeId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" stopColor="#000" />
              <stop offset="0.5" stopColor="#fff" />
              <stop offset="1" stopColor="#000" />
            </linearGradient>
            <mask id={maskId} maskUnits="userSpaceOnUse" x="0" y="0" width="48" height="48">
              <rect width="48" height="48" fill={`url(#${fadeId})`} />
            </mask>
          </defs>
          <path
            d="M17 7h-5a5 5 0 0 0-5 5v24a5 5 0 0 0 5 5h5"
            fill="none"
            stroke="currentColor"
            strokeWidth={3.6}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M31 7h5a5 5 0 0 1 5 5v24a5 5 0 0 1-5 5h-5"
            fill="none"
            stroke="currentColor"
            strokeWidth={3.6}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <rect
            x="19"
            y="8"
            width="10"
            height="32"
            rx="5"
            fill={beam}
            opacity={0.16}
            mask={`url(#${maskId})`}
          />
          <rect
            x="22.2"
            y="3"
            width="3.6"
            height="42"
            rx="1.8"
            fill={beam}
            mask={`url(#${maskId})`}
          />
        </>
      )}
    </svg>
  );
}
