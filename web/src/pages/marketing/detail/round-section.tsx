import {
  Check,
  ChevronRight,
  CircleDashed,
  CircleX,
  Clapperboard,
  Loader2,
  Minus,
  Play,
  RotateCcw,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";

import type {
  AngleOption,
  PlatformOption,
  Project,
  Round,
  StepRun,
  StepRunStatus,
} from "@/api/marketing/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { PLATFORM_ICON } from "@/components/marketing/composer-menus";
import { platformName, STEP_NAME } from "@/utils/marketing/options";
import { methodLabel, renderAllState } from "@/utils/marketing/script";
import { splitStepLevels, STEP_STATUS_LABEL, stepRunOf } from "@/utils/marketing/status";

import { FailedLine, ReverseCard, ReverseSkeleton } from "./reverse-card";
import { ScriptCard } from "./script-card";
import type { MarketingActions } from "./use-actions";

/** 环节小标签的状态图标 */
const STEP_ICON: Record<StepRunStatus, LucideIcon> = {
  queued: CircleDashed,
  running: Loader2,
  succeeded: Check,
  failed: CircleX,
  skipped: Minus,
  canceled: Minus,
  reused: Check,
};

/** 环节小标签的语气颜色 */
const STEP_TONE: Record<StepRunStatus, string> = {
  queued: "text-muted-foreground",
  running: "text-status-running",
  succeeded: "text-status-success",
  reused: "text-status-success",
  failed: "text-destructive",
  skipped: "text-muted-foreground",
  canceled: "text-muted-foreground",
};

/** 环节进度条：第一层事实卡 ∥ 拆解，第二层跟拍 ∥ 创意；每个小标签是环节名 + 状态，悬停写方法 */
function StepsBar({ steps }: { steps: StepRun[] }) {
  const [first, second] = splitStepLevels(steps);
  const chip = (item: StepRun) => {
    const Icon = STEP_ICON[item.status];
    return (
      <span
        key={item.step}
        title={`${STEP_NAME[item.step]}：${STEP_STATUS_LABEL[item.status]} · 方法：${methodLabel(item.method)}`}
        className={cn(
          "bg-chrome-hover ring-chrome-border inline-flex h-7 items-center gap-1.5 rounded-full px-3 text-[12.5px] ring-1",
          STEP_TONE[item.status],
        )}
      >
        <Icon className={cn("size-3.5", item.status === "running" && "animate-spin")} />
        <span className="text-foreground">{STEP_NAME[item.step]}</span>
      </span>
    );
  };
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5" data-slot="marketing-steps">
      {first.map(chip)}
      {first.length > 0 && second.length > 0 && (
        <ChevronRight className="text-muted-foreground size-3.5" aria-hidden />
      )}
      {second.map(chip)}
    </span>
  );
}

/** 轮次下方的一行提示：骨架 / 失败 / 警告都用它 */
function JobLine({
  tone = "plain",
  children,
}: {
  tone?: "plain" | "bad" | "warn";
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "ring-chrome-border flex flex-wrap items-center gap-2 rounded-2xl px-4 py-2.5 text-[13px] ring-1",
        tone === "plain" && "bg-chrome",
        tone === "bad" && "bg-destructive/9",
        tone === "warn" && "bg-status-warning/10",
      )}
    >
      {children}
    </div>
  );
}

/** 一个轮次：轮次头、写脚本中的进度、失败 / 取消 / 警告行、拆解卡和脚本卡 */
export function RoundSection({
  project,
  round,
  angles,
  platforms,
  videoModelLabel,
  available,
  actions,
}: {
  project: Project;
  round: Round;
  angles: AngleOption[];
  platforms: PlatformOption[];
  videoModelLabel: string;
  available: number | null;
  actions: MarketingActions;
}) {
  const settings = round.settings;
  const PlatformIcon = PLATFORM_ICON[settings.platform] ?? PLATFORM_ICON.generic;
  const all = renderAllState(round, available);
  const hasRef = !!round.refVideo;
  const params = settings.params;
  const ratio = (params.aspect_ratio ?? params.ratio) as string | undefined;
  const resolution = params.resolution as string | undefined;
  const duration = params.duration as number | undefined;
  const chips = [
    <span key="plat" className="inline-flex items-center gap-1.5">
      <PlatformIcon className="size-3.5" />
      {platformName(settings.platform, platforms)}
    </span>,
    videoModelLabel,
    [ratio, resolution?.toUpperCase(), duration ? `${duration}s` : undefined]
      .filter(Boolean)
      .join(" · "),
    settings.language === "en" ? "English 旁白" : "中文旁白",
  ].filter(Boolean);
  if (round.refVideo) {
    chips.push(
      <span key="ref" className="inline-flex items-center gap-1.5">
        <Clapperboard className="size-3.5" />
        参考 {Math.round(round.refVideo.duration)}s
      </span>,
    );
  }

  const failedSteps = round.steps.filter((item) => item.status === "failed");
  const followFailed = stepRunOf(round.steps, "follow");
  const createFailed = stepRunOf(round.steps, "create");
  const showSteps = hasRef || failedSteps.length > 0;
  const cancelling = actions.busy[`cancel:${round.id}`];
  const renderingAll = actions.busy[`render-all:${round.id}`];

  const head = (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <span className="font-display text-[15px] font-bold">第 {round.no} 轮</span>
      {chips.map((chip, index) => (
        <span
          key={index}
          className="bg-chrome-hover ring-chrome-border inline-flex h-6 items-center gap-1.5 rounded-full px-2.5 text-xs tabular-nums ring-1"
        >
          {chip}
        </span>
      ))}
      {all.show && (
        <Button
          size="sm"
          className="ml-auto"
          disabled={all.disabled || renderingAll}
          title={all.reason || `一次出 ${all.count} 条`}
          onClick={() => void actions.renderAll(round)}
        >
          {renderingAll ? <Loader2 className="animate-spin" /> : <Play />}
          全部出片
          <span className="tabular-nums opacity-75">✦{all.credits}</span>
        </Button>
      )}
    </div>
  );

  if (round.status === "scripting") {
    const plan = Math.max(round.planCount, 1);
    return (
      <section data-slot="marketing-round" data-status="scripting" className="grid gap-3">
        <div>{head}</div>
        <JobLine>
          <StepsBar steps={round.steps} />
          <span className="text-muted-foreground min-w-0 flex-1">{round.stage}</span>
          <Button
            variant="ghost"
            size="xs"
            disabled={cancelling}
            onClick={() => void actions.cancelRound(round)}
          >
            取消
          </Button>
        </JobLine>
        {hasRef && <ReverseSkeleton />}
        <div className="grid gap-4">
          {Array.from({ length: plan }, (_, i) => (
            <Skeleton key={i} className="h-56 rounded-3xl motion-reduce:animate-none" />
          ))}
        </div>
      </section>
    );
  }

  if (round.status === "failed") {
    const retryable = failedSteps[0];
    return (
      <section data-slot="marketing-round" data-status="failed">
        {head}
        <JobLine tone="bad">
          <StepsBar steps={round.steps} />
          <span className="min-w-0 flex-1">
            {round.error || "写脚本失败"}
            <span className="text-muted-foreground"> · 积分已退回</span>
          </span>
          {retryable && (
            <Button
              variant="outline"
              size="xs"
              disabled={actions.busy[`step:${retryable.id}`]}
              onClick={() => void actions.retryStep(retryable.id)}
            >
              <RotateCcw />
              重试
            </Button>
          )}
        </JobLine>
      </section>
    );
  }

  if (round.status === "canceled") {
    return (
      <section data-slot="marketing-round" data-status="canceled">
        {head}
        <JobLine>
          <span className="text-muted-foreground">已取消，积分已退回</span>
        </JobLine>
      </section>
    );
  }

  return (
    <section data-slot="marketing-round" data-status="ready" className="grid gap-3">
      <div>{head}</div>
      {showSteps && (
        <div className="flex flex-wrap items-center gap-2">
          <StepsBar steps={round.steps} />
        </div>
      )}
      {round.warnings.length > 0 && (
        <JobLine tone="warn">
          <TriangleAlert className="text-status-warning size-4 shrink-0" />
          {round.warnings.join("；")}
        </JobLine>
      )}
      {hasRef && (
        <ReverseCard project={project} round={round} available={available} actions={actions} />
      )}
      <div className="grid gap-4">
        {round.scripts.map((script) => (
          <ScriptCard
            key={script.id}
            project={project}
            round={round}
            script={script}
            angles={angles}
            videoModelLabel={videoModelLabel}
            available={available}
            actions={actions}
          />
        ))}
        {followFailed?.status === "failed" && (
          <div className="bg-chrome ring-chrome-border rounded-3xl ring-1 backdrop-blur-xl">
            <FailedLine
              text={`跟拍版生成失败：${followFailed.error || "原因未知"}`}
              step={followFailed}
              actions={actions}
            />
          </div>
        )}
        {createFailed?.status === "failed" && (
          <div className="bg-chrome ring-chrome-border rounded-3xl ring-1 backdrop-blur-xl">
            <FailedLine
              text={`改编版生成失败：${createFailed.error || "原因未知"}`}
              step={createFailed}
              actions={actions}
            />
          </div>
        )}
      </div>
    </section>
  );
}
