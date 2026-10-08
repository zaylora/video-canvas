import { useEffect, useState } from "react";
import { motion } from "motion/react";

import { CanvasTab } from "@/components/home/canvas-tab";
import { Composer } from "@/components/home/composer";
import { NewsRow } from "@/components/home/news-row";
import { Segmented } from "@/components/home/segmented";
import { SkillRow } from "@/components/home/skill-row";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useComposerStore } from "@/store/composer";
import { takeCarry } from "@/utils/home/carry";
import { greeting } from "@/utils/home/home";

/** 创作页的两种方式：一句话生成，或者直接进画布 */
type CreateTab = "gen" | "canvas";

/**
 * 创作页（设计稿 docs/品牌包装/登录与首页改版原型）：
 * 问候语 + 「生成 / 画布」切换；生成是输入卡片和技能入口，画布是新建与最近画布；底部是「最近上新」。
 * 不放背景视频，视频只留给登录页。
 */
export default function Home() {
  /** 问候语只在进入页面时算一次 */
  const [hello] = useState(() => greeting(new Date().getHours()));
  const [tab, setTab] = useState<CreateTab>("gen");
  const fill = useComposerStore((state) => state.fill);

  /** 登录页「做同款」带来的提示词：只取一次，填进输入卡片并聚焦，不自动提交，由用户确认后再发 */
  useEffect(() => {
    const carried = takeCarry();
    if (carried) fill(carried);
  }, [fill]);

  return (
    <div className="mx-auto w-full max-w-260 px-4 pb-24 md:px-6">
      <div className="flex flex-col items-center gap-6 pt-3 text-center">
        <h1 className="text-2xl font-semibold tracking-tight md:text-[32px]">
          {hello}，今天想要
          <span className="from-foreground to-beam bg-linear-to-r bg-clip-text text-transparent">
            创作
          </span>
          什么？
        </h1>
        <Segmented<CreateTab>
          variant="pill"
          label="创作方式"
          value={tab}
          onChange={setTab}
          options={[
            { value: "gen", label: "生成" },
            { value: "canvas", label: "画布" },
          ]}
        />
      </div>

      <motion.div
        key={tab}
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
      >
        {tab === "gen" ? (
          <>
            <div className="mt-7">
              <Composer />
            </div>
            <SkillRow />
          </>
        ) : (
          <CanvasTab />
        )}
      </motion.div>

      <NewsRow />
    </div>
  );
}
