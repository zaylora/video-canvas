import { useState } from "react";
import { PenLine, Search } from "lucide-react";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { filterSkills } from "@/constants/agent-skills";
import { cn } from "@/lib/utils";
import type { Chip } from "@/utils/agent/chips";

const TABS = [
  { id: "builtin", label: "内置", enabled: true },
  { id: "starred", label: "收藏", enabled: false },
  { id: "mine", label: "我的", enabled: false },
] as const;

/**
 * 插入技能：只有「内置」可用，「收藏」「我的」二期开放（置灰并说明）；按名称、技能名、说明搜索。
 * 点一行插入技能 chip 并关闭弹层。
 */
export function SkillPopover({
  onPick,
  disabled,
}: {
  onPick: (chip: Chip) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const skills = filterSkills(query);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <ChromeTooltip label="插入技能" side="top">
        <PopoverTrigger
          aria-label="插入技能"
          disabled={disabled}
          className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-popup-open:bg-chrome-hover focus-visible:ring-node-ring/60 grid size-8 place-items-center rounded-lg outline-none focus-visible:ring-2 disabled:opacity-40"
        >
          <PenLine className="size-4" />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={8}
        className="w-[min(372px,calc(100vw-24px))] origin-bottom-left gap-2 p-2"
      >
        <div role="tablist" className="flex gap-1">
          {TABS.map((t) => (
            <ChromeTooltip key={t.id} label={t.enabled ? t.label : "二期开放"} side="top">
              <button
                type="button"
                role="tab"
                aria-selected={t.enabled}
                aria-disabled={!t.enabled}
                className={cn(
                  "rounded-md px-2.5 py-1 text-xs outline-none",
                  t.enabled
                    ? "bg-foreground/8 text-foreground"
                    : "text-muted-foreground/60 cursor-not-allowed",
                )}
              >
                {t.label}
              </button>
            </ChromeTooltip>
          ))}
        </div>
        <div className="relative">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索技能"
            aria-label="搜索技能"
            className="h-8 pl-8 text-xs"
          />
        </div>
        {skills.length === 0 ? (
          <p className="text-muted-foreground px-3 py-6 text-center text-xs">没有找到匹配的技能</p>
        ) : (
          <ul className="flex max-h-64 flex-col gap-0.5 overflow-y-auto">
            {skills.map((s) => (
              <li key={s.name}>
                <button
                  type="button"
                  onClick={() => {
                    onPick({ type: "skill", id: s.name, name: s.title });
                    setOpen(false);
                    setQuery("");
                  }}
                  className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex w-full flex-col rounded-lg px-2.5 py-1.5 text-left outline-none focus-visible:ring-2"
                >
                  <span className="text-[13px] font-medium">{s.title}</span>
                  <span className="text-muted-foreground text-xs leading-snug">{s.desc}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}
