import { Sparkles } from "lucide-react";

import { ChromeButton, ChromePill, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { cn } from "@/lib/utils";

/** 右下角的 Agent 入口：浮窗打开时是按下态；没有已发布的 Agent 模型时变淡并说明原因 */
export function AgentLauncher({
  open,
  available,
  running,
  onToggle,
}: {
  /** 浮窗是否打开 */
  open: boolean;
  /** 有没有可用的 Agent 模型；null 是还在加载 */
  available: boolean | null;
  /** Agent 是否正在运行：浮窗收起时用一个小点提示 */
  running: boolean;
  onToggle: () => void;
}) {
  const disabled = available === false;
  return (
    <ChromePill>
      <ChromeTooltip
        label={disabled ? "管理员尚未配置 Agent 模型" : open ? "收起 Agent" : "打开 Agent"}
        shortcut={disabled ? undefined : "⌘/"}
        side="top"
      >
        <ChromeButton
          aria-label="画布 Agent"
          aria-pressed={open}
          aria-disabled={disabled}
          active={open}
          size="lg"
          // 不用 disabled 属性：禁用的按钮不触发 Tooltip，用户就看不到为什么不能点
          className={cn("gap-1.5 px-3 text-[13px] font-medium", disabled && "opacity-40")}
          onClick={() => !disabled && onToggle()}
        >
          <Sparkles className="text-preset" />
          <span>Agent</span>
          {running && !open && (
            <span
              className="bg-status-running size-1.5 animate-pulse rounded-full"
              aria-label="运行中"
            />
          )}
        </ChromeButton>
      </ChromeTooltip>
    </ChromePill>
  );
}
