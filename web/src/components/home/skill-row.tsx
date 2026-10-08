import { motion } from "motion/react";
import { ChevronRight } from "lucide-react";

import { SoonTip } from "@/components/home/soon";
import { SKILL_SHORTCUTS } from "@/constants/creation";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useComposerStore } from "@/store/composer";

/** 技能胶囊的公共样式 */
const PILL =
  "bg-card border-border hover:bg-muted focus-visible:ring-ring/50 inline-flex h-9.5 shrink-0 items-center gap-2 rounded-full border px-4 text-[13px] font-medium whitespace-nowrap outline-none focus-visible:ring-3";

/**
 * 输入卡片下面的技能入口：点一个，把「/技能名 起手句」填进输入框。
 * 窄屏单行横向滚动。「更多技能」要等技能列表接口，先禁用。
 */
export function SkillRow() {
  const fill = useComposerStore((state) => state.fill);

  return (
    <div
      data-slot="skill-row"
      className="-mx-4 mt-4 flex gap-2.5 overflow-x-auto px-4 [scrollbar-width:none] md:mx-0 md:flex-wrap md:justify-center md:overflow-visible md:px-0"
    >
      {SKILL_SHORTCUTS.map((skill) => (
        <motion.button
          key={skill.name}
          type="button"
          whileTap={TAP}
          onClick={() => fill(`/${skill.name} ${skill.text}`)}
          className={PILL}
        >
          / {skill.name}
          {skill.tag && (
            <span
              className={cn(
                "inline-grid h-4.5 place-items-center rounded-md px-1.5 text-[11px] font-medium",
                skill.tag === "new" ? "bg-beam/15 text-beam" : "bg-destructive/15 text-destructive",
              )}
            >
              {skill.tag === "new" ? "新" : "热"}
            </span>
          )}
        </motion.button>
      ))}
      <SoonTip className="inline-flex shrink-0 cursor-not-allowed">
        <button type="button" disabled className={cn(PILL, "text-muted-foreground")}>
          更多技能
          <ChevronRight className="size-3.5" />
        </button>
      </SoonTip>
    </div>
  );
}
