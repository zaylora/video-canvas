import { AnimatePresence, motion, useReducedMotion } from "motion/react";

import { DURATION, EASE_OUT, SKELETON_DELAY } from "@/lib/motion";

type MediaSkeletonProps = {
  /** 内容是否还在加载；变 false 后占位淡出并卸载 */
  pending: boolean;
};

/**
 * 节点预览框里的灰色加载占位：灰底上一道浅色光带缓缓扫过（设计稿 6.12）。
 * 盖在内容层上方：内容到了淡入的同时占位淡出，交叉过渡不露空白。
 * 等 SKELETON_DELAY 才淡入，缓存命中等很快返回的场景不会闪一块灰。
 * 填满父级盒子，圆角跟随父级；减少动态效果时光带不动，只留静态灰底。
 */
export function MediaSkeleton({ pending }: MediaSkeletonProps) {
  const reduce = useReducedMotion();
  return (
    <AnimatePresence>
      {pending && (
        <motion.div
          data-slot="media-skeleton"
          aria-hidden
          className="bg-muted pointer-events-none absolute inset-0 overflow-hidden"
          initial={{ opacity: 0 }}
          animate={{
            opacity: 1,
            transition: { duration: DURATION.base, ease: EASE_OUT, delay: SKELETON_DELAY },
          }}
          exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
        >
          {!reduce && (
            <div className="absolute inset-0 animate-[node-sheen_1.6s_ease-in-out_infinite] bg-[linear-gradient(120deg,transparent_30%,var(--sheen)_50%,transparent_70%)]" />
          )}
        </motion.div>
      )}
    </AnimatePresence>
  );
}
