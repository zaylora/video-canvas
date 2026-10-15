import { useEffect, useState } from "react";
import { motion, useReducedMotion } from "motion/react";

import { CanvasTab } from "@/components/home/canvas-tab";
import { Composer } from "@/components/home/composer";
import { GreetingTitle } from "@/components/home/greeting-title";
import { Segmented } from "@/components/home/segmented";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useComposerStore } from "@/store/composer";
import { takeCarry } from "@/utils/home/carry";
import { greeting } from "@/utils/home/home";

/** 创作页的两种方式：一句话生成，或者直接进画布 */
type CreateTab = "gen" | "canvas";

/**
 * 创作页（设计稿 docs/design/首页生成与对话）：
 * 问候语 + 「生成 / 画布」切换；生成是输入卡片（每次发送都新建一段对话并进入），画布是新建与最近画布。
 * 内容整体按视口高度留出顶部空白，视觉上居中偏上；用 svh 比例而不是把整组内容垂直居中，
 * 这样「生成 / 画布」两个 tab 内容高度不同，标题也不会跟着跳。
 * 静止时有常驻的氛围动效（设计稿 docs/design/首页 第 11 节）：标题后方的光晕、「创作」的流光、输入卡片边缘的流光。
 * 不放背景视频，视频只留给登录页。
 */
export default function Home() {
  /** 问候语只在进入页面时算一次 */
  const [hello] = useState(() => greeting(new Date().getHours()));
  const [tab, setTab] = useState<CreateTab>("gen");
  const fill = useComposerStore((state) => state.fill);
  const reduced = useReducedMotion();
  /** 进入页面时切换控件和内容跟在标题后面依次出现；用户切过 tab 之后换内容不再等 */
  const [switched, setSwitched] = useState(false);
  const rise = reduced ? 0 : 6;

  /** 登录页「做同款」带来的提示词：只取一次，填进输入卡片并聚焦，不自动提交，由用户确认后再发 */
  useEffect(() => {
    const carried = takeCarry();
    if (carried) fill(carried);
  }, [fill]);

  return (
    <div className="mx-auto w-full max-w-260 overflow-x-clip px-4 pt-[clamp(1rem,16svh,9rem)] pb-24 md:px-6">
      <div className="relative isolate flex flex-col items-center gap-6 pt-3 text-center">
        <motion.div
          aria-hidden
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: DURATION.dissolve, ease: EASE_OUT }}
          className="pointer-events-none absolute top-1/2 left-1/2 -z-10 h-105 w-[min(760px,120vw)] -translate-x-1/2 -translate-y-[60%] [--glow:14%] dark:[--glow:18%]"
        >
          <div className="home-aura-blob" />
          <div data-tone="bright" className="home-aura-blob" />
        </motion.div>
        <GreetingTitle hello={hello} />
        <motion.div
          initial={{ opacity: 0, y: rise }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: DURATION.base, ease: EASE_OUT, delay: 0.3 }}
          className="flex w-full justify-center"
        >
          <Segmented<CreateTab>
            variant="pill"
            label="创作方式"
            value={tab}
            onChange={(next) => {
              setSwitched(true);
              setTab(next);
            }}
            options={[
              { value: "gen", label: "生成" },
              { value: "canvas", label: "画布" },
            ]}
          />
        </motion.div>
      </div>

      <motion.div
        key={tab}
        initial={{ opacity: 0, y: rise }}
        animate={{ opacity: 1, y: 0 }}
        transition={{
          duration: DURATION.base,
          ease: EASE_OUT,
          delay: switched ? 0 : 0.36,
        }}
      >
        {tab === "gen" ? (
          <div className="mt-7">
            <Composer target="new" />
          </div>
        ) : (
          <CanvasTab />
        )}
      </motion.div>
    </div>
  );
}
