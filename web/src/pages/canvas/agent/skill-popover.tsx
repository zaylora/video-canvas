import { useState } from "react";
import { PenLine, Search } from "lucide-react";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { Chip } from "@/utils/agent/chips";
import { filterSkills } from "@/utils/agent/skills";

import { AGENT_ICON_BTN, AGENT_POP } from "./styles";
import { useSkillCatalog } from "./use-skill-catalog";

const TABS = [
  { id: "all", label: "全部", enabled: true },
  { id: "starred", label: "收藏", enabled: false },
  { id: "mine", label: "我的", enabled: false },
] as const;

/**
 * 插入技能：只有「全部」（已启用的内置与导入技能）可用，「收藏」「我的」二期开放（置灰并说明）；按名称、技能名、说明搜索。
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
  const { skills: catalog, status, reload } = useSkillCatalog(open);
  const skills = filterSkills(catalog, query);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <ChromeTooltip label="插入技能" side="top">
        <PopoverTrigger
          aria-label="插入技能"
          disabled={disabled}
          className={cn(AGENT_ICON_BTN, "size-8 [&_svg]:size-[17px]")}
        >
          <PenLine className="size-4" />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={8}
        className={cn(AGENT_POP, "w-[min(372px,calc(100vw-24px))] origin-bottom-left gap-2 p-2")}
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
        {status === "loading" ? (
          <div
            className="flex flex-col gap-1.5 px-2.5 py-2"
            aria-busy="true"
            aria-label="正在加载技能"
          >
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-3 w-48" />
            <Skeleton className="mt-1 h-4 w-20" />
            <Skeleton className="h-3 w-40" />
          </div>
        ) : status === "error" ? (
          <p className="text-muted-foreground px-3 py-6 text-center text-xs">
            技能加载失败，
            <button
              type="button"
              onClick={() => void reload()}
              className="text-foreground focus-visible:ring-node-ring/60 rounded underline underline-offset-2 outline-none focus-visible:ring-2"
            >
              重试
            </button>
          </p>
        ) : skills.length === 0 ? (
          <p className="text-muted-foreground px-3 py-6 text-center text-xs">
            {catalog.length === 0 ? "还没有启用的技能" : "没有找到匹配的技能"}
          </p>
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
                  <span className="text-muted-foreground text-xs leading-snug">
                    {s.description}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}
