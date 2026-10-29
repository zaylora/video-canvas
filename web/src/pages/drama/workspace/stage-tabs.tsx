import { motion } from "motion/react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { STAGES, type StageHints, type WorkspaceStage } from "@/utils/drama/stages";

/**
 * 顶部工序标签（总览 · 剧本 · 角色与场景 · 片场 · 成片）。
 * 选中滑块用 layoutId 在标签间弹过去；窄屏保持文字可见。
 * 外观和画布外壳的半透明胶囊（ChromePill / ChromeSegment）同一套。
 */
export function StageTabs({
  stage,
  hints,
  onChange,
}: {
  stage: WorkspaceStage;
  hints: StageHints;
  onChange: (stage: WorkspaceStage) => void;
}) {
  return (
    <div
      role="tablist"
      aria-label="制作工序"
      className="bg-chrome ring-chrome-border mx-auto flex gap-0.5 rounded-full p-1 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150"
    >
      {STAGES.map(({ key, label }, index) => {
        const active = stage === key;
        const hint = hints[key];
        return (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={active}
            aria-label={label}
            title={`${label}（${index + 1}）`}
            onClick={() => onChange(key)}
            className={cn(
              "focus-visible:ring-ring/50 relative flex h-8 items-center gap-1.5 rounded-full px-3.5 text-[13px] font-medium outline-none transition-colors focus-visible:ring-2 max-md:px-2.5",
              active
                ? "text-foreground"
                : "text-muted-foreground hover:bg-chrome-hover hover:text-foreground",
            )}
          >
            {active && (
              <motion.span
                layoutId="drama-stage-active"
                transition={SPRING}
                className="bg-chrome-hover ring-chrome-border absolute inset-0 rounded-full ring-1"
              />
            )}
            <span className="relative">{label}</span>
            {hint && (
              <span
                aria-hidden
                className={cn(
                  "bg-primary relative size-1.5 rounded-full",
                  hint === "run" && "animate-pulse motion-reduce:animate-none",
                )}
              />
            )}
          </button>
        );
      })}
    </div>
  );
}
