import type { CSSProperties } from "react";
import { motion } from "motion/react";

import { cn } from "@/lib/utils";
import type { PeekSide, PeekSize } from "@/utils/canvas/ref-peek";

/** 放大预览从缩略图旁长出来的过渡：0.24 秒，先快后慢 */
const PEEK_TRANSITION = { duration: 0.24, ease: [0.22, 1, 0.36, 1] } as const;

/** 预览里媒体的样式：最大 256×288，圆角、描边、阴影，整幅放进框里不裁切 */
const MEDIA_CLASS =
  "bg-card border-border max-h-72 max-w-64 rounded-xl border object-contain shadow-2xl";

/**
 * 参考图的悬停放大预览：大图，或静音循环播放的视频；从靠近缩略图的一侧为原点缩放加透明度。
 * 首页参考图和画布生成面板共用这一份，样式与动画只在这里改。
 * 只管长相和动画，位置由调用方通过 className / style 给（首页是绝对定位，画布是 portal 后的固定定位）；
 * 不拦鼠标，也要放在 AnimatePresence 里才有退出动画。
 * @param kind 画的是图片还是视频
 * @param src 素材地址
 * @param alt 图片的替代文字
 * @param side 预览在缩略图上方还是下方：决定动画的方向和缩放原点
 * @param originX 缩放原点的水平位置（CSS 长度），默认居中
 * @param className 追加的样式，通常是定位类
 * @param style 追加的内联样式，通常是 left / top / bottom
 * @param onNaturalSize 拿到图片或视频的原始尺寸后回调，调用方据此重新摆位
 */
export function RefPeekCard({
  kind,
  src,
  alt,
  side = "above",
  originX = "50%",
  className,
  style,
  onNaturalSize,
}: {
  kind: "image" | "video";
  src: string;
  alt?: string;
  side?: PeekSide;
  originX?: string;
  className?: string;
  style?: CSSProperties;
  onNaturalSize?: (size: PeekSize) => void;
}) {
  // 从缩略图那一侧长出来：上方的从底部往上，下方的从顶部往下
  const dir = side === "above" ? 1 : -1;
  return (
    <motion.div
      className={cn("pointer-events-none", className)}
      style={{ ...style, transformOrigin: `${originX} ${side === "above" ? "100%" : "0%"}` }}
      initial={{ opacity: 0, scaleY: 0.4, scaleX: 0.85, y: 24 * dir }}
      animate={{ opacity: 1, scaleY: 1, scaleX: 1, y: 0 }}
      exit={{ opacity: 0, scaleY: 0.6, scaleX: 0.9, y: 14 * dir }}
      transition={PEEK_TRANSITION}
    >
      {kind === "video" ? (
        <video
          src={src}
          autoPlay
          muted
          loop
          playsInline
          onLoadedMetadata={(event) =>
            onNaturalSize?.({
              width: event.currentTarget.videoWidth,
              height: event.currentTarget.videoHeight,
            })
          }
          className={MEDIA_CLASS}
        />
      ) : (
        <img
          src={src}
          alt={alt}
          onLoad={(event) =>
            onNaturalSize?.({
              width: event.currentTarget.naturalWidth,
              height: event.currentTarget.naturalHeight,
            })
          }
          className={MEDIA_CLASS}
        />
      )}
    </motion.div>
  );
}
