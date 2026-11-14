import {
  ArrowLeft,
  Check,
  ChevronRight,
  CircleDashed,
  CircleX,
  Clapperboard,
  Loader2,
  Minus,
  PenLine,
  Play,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router";

import type { Project, Round, StepRun, StepRunStatus } from "@/api/marketing/type";
import { ChromePill, ChromeSeparator } from "@/components/canvas/chrome/chrome";
import { PLATFORM_ICON } from "@/components/marketing/composer-menus";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { useMarketingOptions } from "@/hooks/use-marketing-options";
import { cn } from "@/lib/utils";
import { CreditsPill } from "@/pages/canvas/chrome/top-right-bar";
import { platformName, STEP_NAME } from "@/utils/marketing/options";
import { methodLabel, renderAllState } from "@/utils/marketing/script";
import { splitStepLevels, STEP_STATUS_LABEL } from "@/utils/marketing/status";

import { RoundSettingsMenus } from "./round-settings";
import type { MarketingActions } from "./use-actions";

const STEP_ICON: Record<StepRunStatus, LucideIcon> = {
  queued: CircleDashed,
  running: Loader2,
  succeeded: Check,
  failed: CircleX,
  skipped: Minus,
  canceled: Minus,
  reused: Check,
};

const STEP_TONE: Record<StepRunStatus, string> = {
  queued: "text-muted-foreground",
  running: "text-status-running",
  succeeded: "text-status-success",
  reused: "text-status-success",
  failed: "text-destructive",
  skipped: "text-muted-foreground",
  canceled: "text-muted-foreground",
};

/** 环节进度：第一层事实卡 ∥ 拆解，第二层跟拍 ∥ 创意；悬停写状态与方法 */
function StepsBar({ steps }: { steps: StepRun[] }) {
  const [first, second] = splitStepLevels(steps);
  const chip = (item: StepRun) => {
    const Icon = STEP_ICON[item.status];
    return (
      <span
        key={item.step}
        title={`${STEP_NAME[item.step]}：${STEP_STATUS_LABEL[item.status]} · 方法：${methodLabel(item.method)}`}
        className="inline-flex items-center gap-1 px-1 text-[12.5px]"
      >
        <Icon
          className={cn(
            "size-3.5",
            STEP_TONE[item.status],
            item.status === "running" && "animate-spin",
          )}
        />
        {STEP_NAME[item.step]}
      </span>
    );
  };
  return (
    <span data-slot="marketing-steps" className="inline-flex items-center gap-0.5">
      {first.map(chip)}
      {first.length > 0 && second.length > 0 && (
        <ChevronRight className="text-muted-foreground size-3.5" aria-hidden />
      )}
      {second.map(chip)}
    </span>
  );
}

/** 细描边的小胶囊（本轮参数） */
function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="ring-chrome-border text-foreground/85 inline-flex h-8 shrink-0 items-center gap-1 rounded-full px-3 text-[13px] whitespace-nowrap tabular-nums ring-1 [&_svg]:size-3.5">
      {children}
    </span>
  );
}

/**
 * 创作台顶栏：返回、商品名（点开编辑事实卡）、轮次切换（最新在左）、本轮参数（模型与分辨率可改）、环节进度
 * （写脚本中带阶段文字和取消）、全部出片、再写一轮、积分与主题。
 */
export function TopBar({
  project,
  round,
  available,
  actions,
  composerOpen,
  onRound,
  onEditFacts,
  onComposer,
}: {
  project: Project;
  round: Round | undefined;
  available: number | null;
  actions: MarketingActions;
  composerOpen: boolean;
  onRound: (roundId: string) => void;
  onEditFacts: () => void;
  onComposer: () => void;
}) {
  const { platforms } = useMarketingOptions();
  const all = round ? renderAllState(round, available) : null;
  const settings = round?.settings;
  const PlatformIcon = settings
    ? (PLATFORM_ICON[settings.platform] ?? PLATFORM_ICON.generic)
    : null;

  return (
    <header
      data-slot="marketing-top-bar"
      className="bg-chrome ring-chrome-border flex min-h-13 flex-wrap items-center gap-2 rounded-[22px] px-2.5 py-2 ring-1 backdrop-blur-xl"
    >
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="返回营销工作室"
        title="返回营销工作室"
        nativeButton={false}
        render={<Link to="/marketing" />}
      >
        <ArrowLeft />
      </Button>
      <button
        type="button"
        onClick={onEditFacts}
        disabled={!project.product}
        title="编辑商品事实卡"
        className="hover:bg-chrome-hover focus-visible:ring-ring/50 max-w-48 truncate rounded-lg px-1.5 py-0.5 text-[15px] font-bold outline-none focus-visible:ring-2 disabled:hover:bg-transparent"
      >
        {project.title}
      </button>

      {project.rounds.length > 0 && (
        <>
          <ChromeSeparator />
          <ChromePill className="h-9 shadow-none" role="tablist" aria-label="轮次">
            {project.rounds.map((item) => {
              const active = item.id === round?.id;
              return (
                <button
                  key={item.id}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  onClick={() => onRound(item.id)}
                  className={cn(
                    "inline-flex h-7 items-center gap-1 rounded-full px-2.5 text-[12.5px] whitespace-nowrap transition-colors",
                    active
                      ? "bg-chrome-hover ring-chrome-border text-foreground ring-1"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {item.status === "scripting" && <Loader2 className="size-3.5 animate-spin" />}
                  {item.status === "failed" && (
                    <CircleX className="text-destructive size-3.5" />
                  )}第{" "}
                  {item.no} 轮
                </button>
              );
            })}
          </ChromePill>
        </>
      )}

      {round && settings && PlatformIcon && (
        <>
          <ChromeSeparator className="max-xl:hidden" />
          <span className="flex flex-wrap items-center gap-1.5 max-xl:hidden">
            <Chip>
              <PlatformIcon />
              {platformName(settings.platform, platforms)}
            </Chip>
            <RoundSettingsMenus
              project={project}
              round={round}
              actions={actions}
              onComposer={onComposer}
            />
            <Chip>{settings.language === "en" ? "English 旁白" : "中文旁白"}</Chip>
            {round?.refVideo && (
              <Chip>
                <Clapperboard />
                参考 {Math.round(round.refVideo.duration)}s
              </Chip>
            )}
          </span>
        </>
      )}

      {round && round.steps.length > 0 && (
        <>
          <ChromeSeparator />
          <StepsBar steps={round.steps} />
          {round.status === "scripting" && (
            <>
              {round.stage && (
                <span className="text-muted-foreground text-[12.5px]">{round.stage}</span>
              )}
              <Button
                variant="ghost"
                size="xs"
                disabled={actions.busy[`cancel:${round.id}`]}
                onClick={() => void actions.cancelRound(round)}
              >
                取消
              </Button>
            </>
          )}
        </>
      )}

      <span className="flex-1" />
      {round && all?.show && (
        <Button
          size="sm"
          disabled={all.disabled || actions.busy[`render-all:${round.id}`]}
          title={all.reason || `一次出 ${all.count} 条`}
          onClick={() => void actions.renderAll(round)}
        >
          {actions.busy[`render-all:${round.id}`] ? <Loader2 className="animate-spin" /> : <Play />}
          全部出片
          <span className="tabular-nums opacity-70">✦{all.credits}</span>
        </Button>
      )}
      <Button
        variant="outline"
        size="sm"
        aria-expanded={composerOpen}
        onClick={onComposer}
        className={cn(composerOpen && "bg-chrome-hover")}
      >
        <PenLine />
        再写一轮
      </Button>
      <CreditsPill />
      <ThemeToggle />
    </header>
  );
}
