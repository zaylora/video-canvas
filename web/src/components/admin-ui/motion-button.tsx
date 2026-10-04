import { motion } from "motion/react";
import type { ComponentProps } from "react";

import { Button } from "@/components/ui/button";
import { TAP } from "@/lib/motion";

const MotionButtonBase = motion.create(Button);

/**
 * 按下有回弹（whileTap = TAP）的 Button。禁用的按钮收不到指针事件，自然不会回弹；
 * 开了「减少动态效果」时 motion 会自动去掉缩放。
 */
function MotionButton(props: ComponentProps<typeof Button>) {
  return <MotionButtonBase data-slot="motion-button" whileTap={TAP} {...(props as object)} />;
}

export { MotionButton };
