import NumberFlow from "@number-flow/react";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { DEFAULT_BUDGET, useAgentSettings } from "@/store/agent-settings";

import { ProgressRing } from "./progress-ring";
import { AGENT_ICON_BTN, AGENT_POP } from "./styles";

/** 预算的快捷值 */
const QUICK = [20, 50, 100, 200] as const;
const MAX_BUDGET = 100000;

/**
 * 输入框底栏的预算环：本轮已花 / 预算。空闲时环是空的；运行中按比例填充，
 * ≥80% 变警示色、用完变红。点开向上弹出：已花、剩余，以及预算输入框和快捷值；运行中不能改（只能在审批时追加）。
 */
export function BudgetRing({
  spent,
  budget,
  busy,
}: {
  /** 本轮已花，空闲时为 0 */
  spent: number;
  /** 本轮预算：运行中取运行的，空闲时取设置 */
  budget: number;
  busy: boolean;
}) {
  const settings = useAgentSettings();
  const ratio = budget > 0 ? spent / budget : 0;
  const tone =
    ratio >= 1 ? "stroke-destructive" : ratio >= 0.8 ? "stroke-status-warning" : "stroke-credit";
  const set = (value: number) =>
    settings.patch({ budget: Math.max(0, Math.min(MAX_BUDGET, Math.round(value) || 0)) });

  return (
    <Popover>
      <ChromeTooltip label={`本轮 ✦${spent} / ${budget}`}>
        <PopoverTrigger aria-label="本轮积分预算" className={AGENT_ICON_BTN}>
          <ProgressRing value={spent} total={budget} className={tone} />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="top"
        align="end"
        sideOffset={8}
        className={cn(AGENT_POP, "w-[min(240px,calc(100vw-24px))] origin-bottom-right gap-2 p-3")}
      >
        <div className="flex items-end justify-between">
          <div>
            <p className="text-muted-foreground text-xs">已花</p>
            <p className="text-credit text-lg font-semibold tabular-nums">
              ✦ <NumberFlow value={spent} />
            </p>
          </div>
          <div className="text-right">
            <p className="text-muted-foreground text-xs">剩余</p>
            <p className="text-lg font-semibold tabular-nums">
              ✦ <NumberFlow value={Math.max(0, budget - spent)} />
            </p>
          </div>
        </div>
        <label htmlFor="agent-budget" className="text-muted-foreground text-xs">
          本轮预算
        </label>
        <Input
          id="agent-budget"
          type="number"
          min={0}
          max={MAX_BUDGET}
          disabled={busy}
          value={settings.budget}
          onChange={(e) => set(Number(e.target.value))}
          onBlur={() => settings.budget === 0 && settings.patch({ budget: DEFAULT_BUDGET })}
          className="h-8 text-right tabular-nums"
        />
        <div className="flex gap-1">
          {QUICK.map((n) => (
            <button
              key={n}
              type="button"
              disabled={busy}
              onClick={() => set(n)}
              className={cn(
                "ring-chrome-border hover:bg-chrome-hover focus-visible:ring-node-ring/60 h-7 flex-1 rounded-lg text-xs tabular-nums ring-1 outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-40",
                settings.budget === n && "bg-foreground/8",
              )}
            >
              {n}
            </button>
          ))}
        </div>
        <p className="text-muted-foreground text-[11.5px]">
          {busy ? "运行中只能在审批时追加" : "积分，用完会暂停"}
        </p>
      </PopoverContent>
    </Popover>
  );
}
