import NumberFlow from "@number-flow/react";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { contextUsage, formatTokens, type ContextTone } from "@/utils/agent/context-usage";

import { ProgressRing } from "./progress-ring";
import { AGENT_ICON_BTN, AGENT_POP } from "./styles";

/** 环和进度条在各档位下的颜色 */
const TONE: Record<ContextTone, { stroke: string; bar: string }> = {
  normal: { stroke: "stroke-muted-foreground", bar: "bg-muted-foreground" },
  warn: { stroke: "stroke-status-warning", bar: "bg-status-warning" },
  danger: { stroke: "stroke-destructive", bar: "bg-destructive" },
};

/**
 * 输入框底栏的上下文环：当前对话已用多少 / 模型上限。空会话是空环，越用越满；
 * 接近上限变警示色。点开向上弹出：已用、上限、占比，以及一句说明——快满时 Agent 会自动压缩较早的对话。
 * 模型没配上限时只显示已用。
 */
export function ContextRing({ tokens, window }: { tokens: number; window: number }) {
  const { used, known, ratio, percent, tone } = contextUsage(tokens, window);
  const color = TONE[tone];
  const usedText = formatTokens(used);
  const totalText = formatTokens(window);
  const summary = known ? `${usedText} / ${totalText}` : usedText;

  return (
    <Popover>
      <ChromeTooltip label={known ? `上下文 ${summary}（${percent}%）` : `上下文 ${summary}`}>
        <PopoverTrigger aria-label={`上下文用量 ${summary}`} className={AGENT_ICON_BTN}>
          <ProgressRing value={used} total={known ? window : 0} className={color.stroke} />
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
            <p className="text-muted-foreground text-xs">上下文</p>
            <p className="text-lg font-semibold tabular-nums">{usedText}</p>
          </div>
          {known && (
            <div className="text-right">
              <p className="text-muted-foreground text-xs">上限</p>
              <p className="text-lg font-semibold tabular-nums">{totalText}</p>
            </div>
          )}
        </div>
        {known && (
          <>
            <div className="bg-foreground/10 h-1.5 overflow-hidden rounded-full">
              <div
                className={cn(
                  "h-full rounded-full transition-[width,background-color] duration-[240ms] ease-(--motion-ease)",
                  color.bar,
                )}
                style={{ width: `${ratio * 100}%` }}
              />
            </div>
            <p className="text-muted-foreground text-xs tabular-nums">
              已用 <NumberFlow value={percent} />%
            </p>
          </>
        )}
        <p className="text-muted-foreground text-[11.5px]">
          {known ? "快满时会自动压缩较早的对话，不影响继续使用" : "这个模型没有配置上下文上限"}
        </p>
      </PopoverContent>
    </Popover>
  );
}
