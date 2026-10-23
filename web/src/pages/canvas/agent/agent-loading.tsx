import { motion } from "motion/react";

import { Button } from "@/components/ui/button";
import { DURATION, EASE_OUT, SKELETON_DELAY } from "@/lib/motion";

import { ShimmerText } from "./shimmer-text";

/**
 * 打开会话时正文区的加载占位：品牌光球 + 流光文字「正在加载对话…」，在面板里居中。
 * 等 SKELETON_DELAY 才淡入：缓存命中、很快返回时不闪一下；失败时立刻显示原因和「重试」。
 * 加载完才挂载消息流并直接停在底部，所以看不到从上滚到下的过程。
 */
export function AgentLoading({ failed, onRetry }: { failed: boolean; onRetry: () => void }) {
  return (
    <motion.div
      role="status"
      aria-live="polite"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT, delay: failed ? 0 : SKELETON_DELAY }}
      className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2.5 px-5"
    >
      <div aria-hidden className="agent-orb size-8 shrink-0 rounded-full" />
      {failed ? (
        <>
          <p className="text-muted-foreground text-[12.5px]">对话加载失败</p>
          <Button size="sm" variant="outline" onClick={onRetry}>
            重试
          </Button>
        </>
      ) : (
        <ShimmerText className="text-[12.5px]">正在加载对话…</ShimmerText>
      )}
    </motion.div>
  );
}
