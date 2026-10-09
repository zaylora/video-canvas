import { motion } from "motion/react";
import type { ReactNode } from "react";

import { DURATION, EASE_OUT, STAGGER, STAGGER_MAX } from "@/lib/motion";

/**
 * 入场：自下往上浮起（opacity 0→1，translateY 8px→0），按 index 依次错开 STAGGER，
 * 超过 STAGGER_MAX 的不再往后错，免得内容多了要等太久。只在挂载时播放一次，数据刷新不会重播。
 * 包在需要入场的块外面；减少动态效果时由外层 MotionConfig 降级为只淡入。
 * @param index 在同一屏里的先后顺序，从 0 开始
 */
function Reveal({
  index = 0,
  className,
  children,
}: {
  index?: number;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <motion.div
      data-slot="reveal"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        duration: DURATION.base,
        ease: EASE_OUT,
        delay: Math.min(index, STAGGER_MAX) * STAGGER,
      }}
      className={className}
    >
      {children}
    </motion.div>
  );
}

export { Reveal };
