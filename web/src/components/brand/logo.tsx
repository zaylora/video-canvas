import { useId, type SVGProps } from "react";

import { cn } from "@/lib/utils";

/** 小于等于这个尺寸（px）换成小尺寸版：笔尖加粗、光束改实色，不做两端渐隐 */
const SMALL_MAX = 24;

/**
 * 「流光」的三笔，48×48 坐标。毛笔弧和 S 形光都是按笔宽曲线算出来的填充轮廓，
 * 生成脚本在 docs/design/品牌Logo/logo-gen.py：要调形状改那里的骨架点再重新生成，不要手改 path。
 */
const STROKES = {
  large: {
    left: "M16.8 4.6L14.9 5.5L13.2 6.6L11.5 7.8L10.0 9.1L8.5 10.5L7.2 12.1L6.0 13.9L5.0 15.7L4.2 17.7L3.6 19.7L3.2 21.8L3.1 24.0L3.2 26.1L3.6 28.2L4.2 30.3L5.0 32.2L6.0 34.1L7.2 35.8L8.5 37.4L10.0 38.8L11.5 40.1L13.2 41.3L14.9 42.4L16.8 43.3L16.9 43.3L17.0 43.4L17.1 43.3L17.2 43.2L17.3 43.1L17.3 43.0L17.4 42.9L17.3 42.8L17.2 42.7L17.1 42.6L15.4 41.6L13.9 40.4L12.5 39.1L11.4 37.6L10.5 36.0L9.8 34.3L9.3 32.6L8.9 30.9L8.6 29.2L8.4 27.4L8.3 25.7L8.3 24.0L8.3 22.2L8.4 20.5L8.6 18.8L8.9 17.0L9.3 15.3L9.8 13.6L10.5 11.9L11.4 10.3L12.5 8.8L13.9 7.5L15.4 6.3L17.1 5.3L17.2 5.2L17.3 5.1L17.4 5.0L17.3 4.9L17.3 4.8L17.2 4.7L17.1 4.6L17.0 4.6L16.9 4.6Z",
    right:
      "M30.8 5.3L32.5 6.3L34.1 7.5L35.4 8.8L36.5 10.3L37.4 11.9L38.1 13.6L38.6 15.3L39.0 17.0L39.3 18.8L39.5 20.5L39.6 22.2L39.6 24.0L39.6 25.7L39.5 27.4L39.3 29.2L39.0 30.9L38.6 32.6L38.1 34.3L37.4 36.0L36.5 37.6L35.4 39.1L34.1 40.4L32.5 41.6L30.8 42.6L30.7 42.7L30.6 42.8L30.6 42.9L30.6 43.0L30.6 43.1L30.7 43.2L30.8 43.3L30.9 43.4L31.0 43.3L31.1 43.3L33.0 42.4L34.7 41.3L36.4 40.1L37.9 38.8L39.4 37.4L40.7 35.8L41.9 34.1L42.9 32.2L43.7 30.3L44.3 28.2L44.7 26.1L44.8 24.0L44.7 21.8L44.3 19.7L43.7 17.7L42.9 15.7L41.9 13.9L40.7 12.1L39.4 10.5L37.9 9.1L36.4 7.8L34.7 6.6L33.0 5.5L31.1 4.6L31.0 4.6L30.9 4.6L30.8 4.6L30.7 4.7L30.6 4.8L30.6 4.9L30.6 5.0L30.6 5.1L30.7 5.2Z",
    beam: "M26.7 2.7L25.2 4.3L24.0 5.8L22.9 7.4L22.1 9.0L21.5 10.8L21.1 12.5L20.8 14.4L20.7 16.3L20.8 18.2L21.0 20.0L21.3 21.9L21.7 23.7L22.2 25.5L22.7 27.3L23.2 29.0L23.6 30.7L24.0 32.4L24.3 34.1L24.3 35.8L24.2 37.6L23.8 39.4L23.2 41.2L22.1 43.0L20.7 44.8L20.7 44.8L20.7 44.9L20.7 45.0L20.7 45.1L20.8 45.2L20.8 45.2L20.9 45.3L21.0 45.2L21.1 45.2L21.2 45.1L22.7 43.4L23.9 41.6L25.0 39.9L25.8 38.0L26.4 36.2L26.8 34.2L27.1 32.3L27.2 30.3L27.1 28.4L26.9 26.4L26.6 24.5L26.2 22.7L25.7 20.9L25.2 19.2L24.7 17.5L24.3 15.9L23.9 14.3L23.7 12.7L23.6 11.1L23.7 9.5L24.1 7.9L24.7 6.3L25.8 4.7L27.2 3.2L27.2 3.1L27.3 3.0L27.3 2.9L27.2 2.8L27.2 2.7L27.1 2.7L27.0 2.7L26.9 2.7L26.8 2.7Z",
  },
  small: {
    left: "M16.2 3.6L14.4 4.6L12.5 5.7L10.8 7.0L9.2 8.4L7.7 10.0L6.4 11.7L5.3 13.5L4.3 15.4L3.5 17.5L2.9 19.6L2.5 21.8L2.4 24.0L2.5 26.2L2.9 28.3L3.5 30.4L4.3 32.5L5.3 34.4L6.4 36.2L7.7 37.9L9.2 39.5L10.8 40.9L12.5 42.2L14.4 43.3L16.2 44.3L16.7 44.4L17.2 44.4L17.6 44.3L18.0 44.0L18.3 43.7L18.4 43.2L18.4 42.7L18.3 42.3L18.0 41.9L17.7 41.6L16.0 40.7L14.5 39.6L13.3 38.3L12.2 36.9L11.3 35.4L10.6 33.9L10.1 32.3L9.6 30.6L9.3 29.0L9.1 27.3L9.0 25.6L9.0 24.0L9.0 22.3L9.1 20.6L9.3 18.9L9.6 17.3L10.1 15.6L10.6 14.0L11.3 12.5L12.2 11.0L13.3 9.6L14.5 8.3L16.0 7.2L17.7 6.3L18.0 6.0L18.3 5.6L18.4 5.2L18.4 4.7L18.3 4.2L18.0 3.9L17.6 3.6L17.2 3.5L16.7 3.5Z",
    right:
      "M30.2 6.3L32.0 7.2L33.4 8.3L34.6 9.6L35.7 11.0L36.6 12.5L37.3 14.0L37.9 15.6L38.3 17.3L38.6 18.9L38.8 20.6L38.9 22.3L38.9 24.0L38.9 25.6L38.8 27.3L38.6 29.0L38.3 30.6L37.9 32.3L37.3 33.9L36.6 35.4L35.7 36.9L34.6 38.3L33.4 39.6L32.0 40.7L30.2 41.6L29.9 41.9L29.6 42.3L29.5 42.7L29.5 43.2L29.6 43.7L29.9 44.0L30.3 44.3L30.7 44.4L31.2 44.4L31.7 44.3L33.5 43.3L35.4 42.2L37.1 40.9L38.7 39.5L40.2 37.9L41.5 36.2L42.6 34.4L43.6 32.5L44.4 30.4L45.0 28.3L45.4 26.2L45.5 24.0L45.4 21.8L45.0 19.6L44.4 17.5L43.6 15.4L42.6 13.5L41.5 11.7L40.2 10.0L38.7 8.4L37.1 7.0L35.4 5.7L33.5 4.6L31.7 3.6L31.2 3.5L30.7 3.5L30.3 3.6L29.9 3.9L29.6 4.2L29.5 4.7L29.5 5.2L29.6 5.6L29.9 6.0Z",
    beam: "M26.0 2.1L24.5 3.6L23.1 5.3L22.1 7.0L21.2 8.8L20.6 10.6L20.2 12.5L20.0 14.4L20.0 16.4L20.1 18.3L20.3 20.2L20.6 22.1L21.0 23.9L21.5 25.7L22.0 27.4L22.4 29.1L22.9 30.8L23.2 32.4L23.4 34.1L23.5 35.7L23.3 37.4L23.0 39.0L22.3 40.7L21.3 42.4L20.0 44.1L19.7 44.5L19.7 44.9L19.7 45.3L19.9 45.7L20.1 46.0L20.5 46.2L20.9 46.3L21.3 46.2L21.7 46.1L22.0 45.8L23.5 44.0L24.8 42.1L25.9 40.2L26.7 38.3L27.3 36.3L27.7 34.3L27.9 32.2L27.9 30.2L27.8 28.2L27.6 26.3L27.3 24.4L26.9 22.5L26.4 20.7L25.9 19.0L25.5 17.4L25.1 15.8L24.7 14.2L24.5 12.7L24.4 11.2L24.6 9.8L24.9 8.3L25.6 6.8L26.5 5.3L27.9 3.9L28.1 3.5L28.2 3.1L28.2 2.7L28.1 2.3L27.9 2.0L27.5 1.8L27.1 1.7L26.7 1.7L26.3 1.8Z",
  },
} as const;

type LogoProps = Omit<SVGProps<SVGSVGElement>, "viewBox" | "width" | "height"> & {
  size?: number;
  bright?: boolean;
  animated?: boolean;
};

/** 光束的竖向渐变：中间实、两端淡到 15%，像一缕光从中间亮起 */
function BeamGradient({ id, beam }: { id: string; beam: string }) {
  return (
    <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stopColor={beam} stopOpacity={0.15} />
      <stop offset="0.5" stopColor={beam} />
      <stop offset="1" stopColor={beam} stopOpacity={0.15} />
    </linearGradient>
  );
}

/**
 * 「连镜」的 Logo「流光」：两道毛笔弧是取景框，中间一缕琥珀色的 S 形光穿过。
 * 弧用 currentColor 跟随文字色；光用品牌 token，深浅主题各取一档，
 * 压在视频上的场景传 bright 用永远偏亮的那一档。
 * @param size 边长（px），不超过 24 时自动换小尺寸版
 * @param bright 光用 beam-bright：登录页舞台这类永远偏暗的底
 * @param animated 拆成光晕、左弧、右弧、光束四层叠放，供加载层的「开镜 · 对焦 · 光铺开」
 *   分别做 transform 动画（index.css 的 logo-anim-*）；每层外面的 span 管退场，里面的 svg 管进场和对焦循环，
 *   两层的 transform 叠加，退场从循环当时的位置接着走
 */
export function Logo({
  size = 32,
  bright = false,
  animated = false,
  className,
  ...props
}: LogoProps) {
  const uid = useId();
  const gradientId = `${uid}-beam`;
  const beam = bright ? "var(--beam-bright)" : "var(--beam)";
  const variant = size <= SMALL_MAX && !animated ? "small" : "large";
  const strokes = STROKES[variant];

  if (animated) {
    const layer = "absolute inset-0 size-full overflow-visible";
    return (
      <span
        aria-hidden="true"
        data-slot="logo"
        className={cn("logo-anim relative inline-block shrink-0", className)}
        style={{ width: size, height: size }}
      >
        <span data-part="glow" className={layer}>
          <svg viewBox="0 0 48 48" className={cn(layer, "blur-[6px]")}>
            <path d={strokes.beam} fill={beam} />
          </svg>
        </span>
        <span data-part="left" className={layer}>
          <svg viewBox="0 0 48 48" className={layer}>
            <path d={strokes.left} fill="currentColor" />
          </svg>
        </span>
        <span data-part="right" className={layer}>
          <svg viewBox="0 0 48 48" className={layer}>
            <path d={strokes.right} fill="currentColor" />
          </svg>
        </span>
        <span data-part="beam" className={layer}>
          <svg viewBox="0 0 48 48" className={layer}>
            <defs>
              <BeamGradient id={gradientId} beam={beam} />
            </defs>
            <path d={strokes.beam} fill={`url(#${gradientId})`} />
          </svg>
        </span>
      </span>
    );
  }

  return (
    <svg
      viewBox="0 0 48 48"
      width={size}
      height={size}
      aria-hidden="true"
      data-slot="logo"
      data-variant={variant}
      className={cn("shrink-0", className)}
      {...props}
    >
      {variant === "large" && (
        <defs>
          <BeamGradient id={gradientId} beam={beam} />
        </defs>
      )}
      <path d={strokes.left} fill="currentColor" />
      <path d={strokes.right} fill="currentColor" />
      <path d={strokes.beam} fill={variant === "large" ? `url(#${gradientId})` : beam} />
    </svg>
  );
}
