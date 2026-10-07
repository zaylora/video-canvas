import { UploadCloud } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";

import { DURATION, EASE_OUT } from "@/lib/motion";

/**
 * 全页拖放提示：拖入含文件的内容时整页出现虚线框，只用一层 backdrop-blur。
 * 进入 base 档、退出 exit 档（opacity + scale 0.98→1）；减少动态效果时由页面的 MotionConfig 去掉缩放。
 * @param active 是否显示（对话框已打开时由页面传 false，对话框内的拖放区自己高亮）
 */
export function DropOverlay({ active }: { active: boolean }) {
  return (
    <AnimatePresence>
      {active && (
        <motion.div
          data-slot="drop-overlay"
          aria-hidden="true"
          initial={{ opacity: 0, scale: 0.98 }}
          animate={{
            opacity: 1,
            scale: 1,
            transition: { duration: DURATION.base, ease: EASE_OUT },
          }}
          exit={{
            opacity: 0,
            scale: 0.98,
            transition: { duration: DURATION.exit, ease: EASE_OUT },
          }}
          className="border-primary/45 bg-background/80 pointer-events-none fixed inset-3 z-40 flex flex-col items-center justify-center gap-2 rounded-2xl border-2 border-dashed backdrop-blur-sm"
        >
          <span className="bg-primary text-primary-foreground grid size-14 place-items-center rounded-2xl">
            <UploadCloud className="size-6" />
          </span>
          <b className="text-lg">松开以导入技能包</b>
          <span className="text-muted-foreground text-sm">
            支持 .zip 压缩包、文件夹或单个 SKILL.md
          </span>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
