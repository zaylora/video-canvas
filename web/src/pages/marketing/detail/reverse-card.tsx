import { useEffect, useState } from "react";
import {
  BookOpen,
  ChevronDown,
  ChevronUp,
  CircleX,
  Copyright,
  Flame,
  Loader2,
  Megaphone,
  Play,
  RefreshCw,
  RotateCcw,
  Undo2,
} from "lucide-react";
import { toast } from "sonner";

import { quoteMarketing } from "@/api/marketing";
import type { Project, ProductRole, Round, StepRun } from "@/api/marketing/type";
import { EditableText } from "@/components/marketing/editable-text";
import { StatusPill } from "@/components/drama/status-pill";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { PRODUCT_ROLE_NAME } from "@/utils/marketing/options";
import {
  patchReverseShot,
  refollowState,
  reverseEditable,
  reverseSummary,
  validateAction,
} from "@/utils/marketing/reverse";
import { methodLabel } from "@/utils/marketing/script";
import { stepRunOf } from "@/utils/marketing/status";

import type { MarketingActions } from "./use-actions";

/** 参考视频缩略图：第一帧 + 播放标 */
function RefThumb({ url }: { url: string | undefined }) {
  return (
    <span className="bg-muted relative block h-12 w-9 shrink-0 overflow-hidden rounded-lg">
      {url && <video src={url} preload="metadata" muted className="size-full object-cover" />}
      <span className="absolute inset-0 grid place-items-center text-white [&_svg]:size-3.5 [&_svg]:fill-white/90">
        <Play />
      </span>
    </span>
  );
}

/** 重写跟拍版的积分：拆解有改动时按本轮设置问一次预估，没问到就不显示数字 */
function useRefollowCost(project: Project, round: Round): number | null {
  const [cost, setCost] = useState<number | null>(null);
  const dirty = round.reverseDirty;
  useEffect(() => {
    if (!dirty) return;
    let alive = true;
    quoteMarketing({ projectId: project.id, hasRef: true, settings: round.settings })
      .then((quote) => {
        if (alive) setCost(quote.steps.find((item) => item.step === "follow")?.credits ?? null);
      })
      .catch(() => alive && setCost(null));
    return () => {
      alive = false;
    };
  }, [dirty, project.id, round.settings]);
  return dirty ? cost : null;
}

/** 环节失败的一行：原因 + 重试，只重跑这个环节 */
function FailedLine({
  text,
  step,
  actions,
  label = "重试",
}: {
  text: string;
  step: StepRun;
  actions: MarketingActions;
  label?: string;
}) {
  const busy = actions.busy[`step:${step.id}`];
  return (
    <div className="flex items-center gap-2 px-4 py-3 text-[13px]">
      <CircleX className="text-destructive size-4 shrink-0" />
      <span className="min-w-0 flex-1">{text}</span>
      <Button
        variant="outline"
        size="xs"
        disabled={busy}
        onClick={() => void actions.retryStep(step.id)}
      >
        {busy ? <Loader2 className="animate-spin" /> : <RotateCcw />}
        {label}
      </Button>
    </div>
  );
}

/**
 * 参考拆解卡（有参考视频时，轮次头下方，默认折叠）：
 * 折叠是一行摘要（镜头数、时长、为什么火、品牌 / 背书提示）；展开后是镜头表，
 * 动作、台词、商品怎么出现可改（失焦即保存，服务端校验；跟拍版出片中时只读），
 * 有改动时可「按拆解重写跟拍版」，随时可「恢复原稿」。拆解失败时这里显示原因和「重试拆解」。
 */
export function ReverseCard({
  project,
  round,
  available,
  actions,
}: {
  project: Project;
  round: Round;
  available: number | null;
  actions: MarketingActions;
}) {
  const [open, setOpen] = useState(false);
  const step = stepRunOf(round.steps, "reverse");
  const cost = useRefollowCost(project, round);
  if (!step) return null;

  const wrapper = "bg-chrome ring-chrome-border rounded-3xl ring-1 backdrop-blur-xl";

  if (step.status === "failed") {
    return (
      <div className={wrapper}>
        <FailedLine
          text={`参考片拆解失败：${step.error || "原因未知"}（跟拍版未生成，改编版照常）`}
          step={step}
          actions={actions}
          label="重试拆解"
        />
      </div>
    );
  }
  if (!round.reverse) {
    if (step.status === "running" || step.status === "queued") {
      return (
        <div
          className={cn(
            wrapper,
            "text-muted-foreground flex items-center gap-2 px-4 py-4 text-[13px]",
          )}
        >
          <Loader2 className="size-4 animate-spin" />
          正在拆解参考片
        </div>
      );
    }
    return null;
  }

  const reverse = round.reverse;
  const summary = reverseSummary(reverse);
  const editable = reverseEditable(round);
  const refollow = refollowState(round, null, available, cost);
  const refollowing = actions.busy[`refollow:${round.id}`];
  const saving = actions.busy[`reverse:${round.id}`];
  const modified = round.reverseEdited || round.reverseDirty;

  const commit = (
    index: number,
    patch: Parameters<typeof patchReverseShot>[2],
    field?: "action",
  ) => {
    if (field === "action") {
      const problem = validateAction(patch.action ?? "");
      if (problem) {
        toast.error(problem);
        return;
      }
    }
    void actions.saveReverse(round, patchReverseShot(reverse, index, patch));
  };

  return (
    <div className={wrapper} data-slot="marketing-reverse-card">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="focus-visible:ring-ring/50 flex w-full items-center gap-3 rounded-3xl px-4 py-3 text-left outline-none focus-visible:ring-2"
      >
        <RefThumb url={round.refVideo?.url} />
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-1.5">
            <b className="text-sm">参考拆解</b>
            <span className="text-muted-foreground text-xs tabular-nums">
              {summary.shotCount} 个镜头 · {round.refVideo?.duration ?? summary.seconds}s
            </span>
            {modified && <StatusPill tone="warning">已修改</StatusPill>}
            {step.method && (
              <span className="text-muted-foreground inline-flex items-center gap-1 text-xs">
                <BookOpen className="size-3.5" />
                方法：{methodLabel(step.method)}
              </span>
            )}
          </span>
          {summary.whyViral && (
            <span className="text-muted-foreground mt-0.5 flex items-start gap-1 text-[12.5px] leading-snug">
              <Flame className="mt-0.5 size-3.5 shrink-0" />
              <span className="line-clamp-2">{summary.whyViral}</span>
            </span>
          )}
        </span>
        <span className="hidden shrink-0 items-center gap-1.5 sm:flex">
          {summary.brandCount > 0 && (
            <StatusPill tone="destructive">
              <span title={reverse.brands.join("、")} className="inline-flex items-center gap-1">
                <Copyright />
                第三方品牌 {summary.brandCount}
              </span>
            </StatusPill>
          )}
          {summary.claimCount > 0 && (
            <StatusPill tone="warning">
              <span title={reverse.claims.join("、")} className="inline-flex items-center gap-1">
                <Megaphone />
                原片背书 {summary.claimCount}
              </span>
            </StatusPill>
          )}
        </span>
        {open ? (
          <ChevronUp className="size-4 shrink-0" />
        ) : (
          <ChevronDown className="size-4 shrink-0" />
        )}
      </button>

      {open && (
        <div className="border-chrome-border border-t px-4 pb-3">
          <div className="text-muted-foreground py-2.5 text-[12.5px]">
            {[reverse.overall.style, reverse.overall.narrativeArc].filter(Boolean).join(" · ")}
          </div>
          <div className="grid gap-2">
            {reverse.shots.map((shot, index) => (
              <div
                key={shot.id}
                className="border-chrome-border grid grid-cols-[5.5rem_1fr_11rem] items-start gap-3 border-t pt-2 max-md:grid-cols-1"
              >
                <div className="text-muted-foreground pt-1 text-xs tabular-nums">
                  {shot.start}–{shot.end}s
                  <b className="text-foreground/80 block font-medium">
                    {shot.shotSize} · {shot.camera}
                  </b>
                  {shot.isOpening3s && (
                    <StatusPill tone="muted" className="bg-beam/15 text-beam mt-1">
                      前 3 秒
                    </StatusPill>
                  )}
                </div>
                <div className="min-w-0">
                  <div className="text-muted-foreground px-2 text-[11px]">主体 + 动作</div>
                  <EditableText
                    multiline
                    value={shot.action}
                    ariaLabel={`镜头 ${shot.id} 主体和动作`}
                    disabled={!editable || saving}
                    onCommit={(value) => commit(index, { action: value }, "action")}
                  />
                  <div className="text-muted-foreground px-2 pt-1 text-[11px]">台词</div>
                  <EditableText
                    value={shot.dialogue}
                    ariaLabel={`镜头 ${shot.id} 台词`}
                    disabled={!editable || saving}
                    onCommit={(value) => commit(index, { dialogue: value })}
                  />
                  {shot.keyColors.length > 0 && (
                    <div className="flex flex-wrap gap-1 px-2 pt-1">
                      {shot.keyColors.map((color) => (
                        <span
                          key={color}
                          className="bg-chrome-hover ring-chrome-border rounded-full px-2 text-[11.5px] leading-5 ring-1"
                        >
                          {color}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
                <div className="min-w-0">
                  <div className="text-muted-foreground px-2 text-[11px]">商品怎么出现</div>
                  <select
                    value={shot.productRole}
                    disabled={!editable || saving}
                    aria-label={`镜头 ${shot.id} 商品怎么出现`}
                    onChange={(event) =>
                      commit(index, { productRole: event.target.value as ProductRole })
                    }
                    className="bg-chrome-hover ring-chrome-border focus-visible:ring-ring/50 mt-1 h-8 w-full rounded-full px-3 text-[13px] ring-1 outline-none focus-visible:ring-2 disabled:opacity-60"
                  >
                    {(Object.keys(PRODUCT_ROLE_NAME) as ProductRole[]).map((role) => (
                      <option key={role} value={role}>
                        {PRODUCT_ROLE_NAME[role]}
                      </option>
                    ))}
                  </select>
                  <div className="text-muted-foreground px-2 pt-1 text-[11.5px]">
                    {shot.productInFrame}
                  </div>
                </div>
              </div>
            ))}
          </div>
          <div className="border-chrome-border mt-2 flex flex-wrap items-center gap-2 border-t pt-3">
            {reverse.claims.length > 0 && (
              <span className="text-muted-foreground text-xs">
                原片背书：{reverse.claims.join("、")}
              </span>
            )}
            <span className="flex-1" />
            <Button
              variant="ghost"
              size="xs"
              disabled={
                !round.reverseEdited || !editable || actions.busy[`reverse-restore:${round.id}`]
              }
              onClick={() => void actions.restoreReverse(round)}
            >
              <Undo2 />
              恢复原稿
            </Button>
            <Button
              size="xs"
              disabled={refollow.disabled || refollowing}
              title={refollow.reason}
              onClick={() => void actions.refollow(round)}
            >
              {refollowing ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              按拆解重写跟拍版
              {cost !== null && <span className="tabular-nums opacity-70">✦{cost}</span>}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

/** 拆解卡的占位骨架：写脚本中且有参考视频时显示 */
export function ReverseSkeleton() {
  return <Skeleton className="h-19.5 rounded-3xl motion-reduce:animate-none" />;
}

export { FailedLine };
