import { motion } from "motion/react";
import { Clapperboard, Film, LayoutGrid, ScrollText, Users, type LucideIcon } from "lucide-react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { STAGES, type StageHints, type WorkspaceStage } from "@/utils/drama/stages";

const ICONS: Record<WorkspaceStage, LucideIcon> = {
  overview: LayoutGrid,
  script: ScrollText,
  assets: Users,
  studio: Clapperboard,
  final: Film,
};

/**
 * 顶部工序标签（总览 · 剧本 · 角色与场景 · 片场 · 成片）。
 * 选中滑块用 layoutId 在标签间弹过去；窄屏只留图标，文字放进 aria-label。
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
      className="bg-card border-border mx-auto flex gap-0.5 rounded-xl border p-[3px]"
    >
      {STAGES.map(({ key, label }, index) => {
        const Icon = ICONS[key];
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
              "focus-visible:ring-ring/50 relative flex h-8 items-center gap-1.5 rounded-lg px-3 text-[13px] font-medium outline-none transition-colors focus-visible:ring-2 max-md:px-2.5 [&_svg]:size-4",
              active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {active && (
              <motion.span
                layoutId="drama-stage-active"
                transition={SPRING}
                className="bg-muted absolute inset-0 rounded-lg shadow-[0_1px_0_oklch(1_0_0/0.06)_inset]"
              />
            )}
            <Icon className="relative" aria-hidden />
            <span className="relative max-lg:hidden">{label}</span>
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
