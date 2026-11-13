import {
  AlertTriangle,
  CircleX,
  Clapperboard,
  Copy,
  FileX,
  Film,
  Loader2,
  RotateCcw,
} from "lucide-react";

import type { Project, Round, Script } from "@/api/marketing/type";
import { useMarketingOptions } from "@/hooks/use-marketing-options";
import { cn } from "@/lib/utils";
import { useWorkbenchStore } from "@/store/marketing-workbench";
import { angleName } from "@/utils/marketing/options";
import { playableTakes } from "@/utils/marketing/script";
import { stepRunOf } from "@/utils/marketing/status";
import { formatSeconds } from "@/utils/marketing/workbench";

import { FactsPanel } from "./facts";
import { useRenderProgress } from "./hooks";
import type { MarketingActions } from "./use-actions";

/** 列表项外壳：选中时浅底 + 左侧紫色竖条 */
function Item({
  selected,
  onSelect,
  children,
}: {
  selected: boolean;
  onSelect: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-current={selected || undefined}
      onClick={onSelect}
      className={cn(
        "focus-visible:ring-ring/50 relative flex w-full items-center gap-2.5 rounded-[18px] py-2 pr-2.5 pl-3 text-left outline-none transition-colors duration-150 focus-visible:ring-2",
        selected
          ? "bg-beam/10 before:bg-beam before:absolute before:inset-y-3 before:left-0.75 before:w-0.75 before:rounded-full"
          : "hover:bg-chrome-hover",
      )}
    >
      {children}
    </button>
  );
}

/** 9:16 小缩略图 */
function Mini({ children, className }: { children?: React.ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        "bg-muted relative block h-15 w-9 shrink-0 overflow-hidden rounded-lg",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** 一份脚本的状态角标：已出片绿点（多条 ×N）、待出片灰点、旁白有问题黄三角、出片中百分比、重写中转圈、失败红叉 */
function ScriptStatus({ script }: { script: Script }) {
  const progress = useRenderProgress(script);
  const takes = playableTakes(script).length;
  switch (script.status) {
    case "done":
      return (
        <span
          className="text-muted-foreground inline-flex items-center gap-1 text-[11.5px] tabular-nums"
          title="已出片"
        >
          <i className="bg-status-success size-1.75 rounded-full" />
          {takes > 1 && `×${takes}`}
        </span>
      );
    case "rendering":
      return (
        <span className="text-status-running font-mono text-[11.5px] tabular-nums" title="出片中">
          {Math.round(progress ?? 0)}%
        </span>
      );
    case "rewriting":
      return <Loader2 className="text-status-running size-3.5 animate-spin" aria-label="重写中" />;
    case "failed":
      return <CircleX className="text-destructive size-3.5" aria-label="出片失败" />;
    default:
      return script.flags.length > 0 ? (
        <AlertTriangle className="text-status-warning size-3.5" aria-label="旁白需修改" />
      ) : (
        <i className="bg-muted-foreground size-1.75 rounded-full" title="待出片" />
      );
  }
}

/**
 * 左栏「素材与成片」（创作台左栏）：本轮原片 + 每份脚本一行（缩略图、角度、标题、状态），
 * 没写出来的环节显示虚线行 + 重试；写脚本中显示骨架。下面是可折叠的商品信息。
 */
export function AssetList({
  project,
  round,
  actions,
  onEditFacts,
}: {
  project: Project;
  round: Round | undefined;
  actions: MarketingActions;
  onEditFacts: () => void;
}) {
  const { angles } = useMarketingOptions();
  const selection = useWorkbenchStore((state) => state.selection);
  const select = useWorkbenchStore((state) => state.select);
  const productUrl = project.productAssets[0]?.url ?? "";

  const reverseStep = round ? stepRunOf(round.steps, "reverse") : undefined;
  const failedSteps = round
    ? round.steps.filter(
        (step) => (step.step === "follow" || step.step === "create") && step.status === "failed",
      )
    : [];

  return (
    <aside
      data-slot="marketing-asset-list"
      className="bg-chrome ring-chrome-border flex min-h-0 flex-col overflow-hidden rounded-[28px] ring-1 backdrop-blur-xl"
    >
      <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto [scrollbar-gutter:stable]">
        <div className="text-muted-foreground flex items-center gap-1.5 px-4 pt-3.5 pb-2 text-xs">
          <Film className="size-3.5" />
          {round ? `第 ${round.no} 轮 · 素材与成片` : "素材与成片"}
        </div>
        {round && round.warnings.length > 0 && (
          <p className="text-status-warning mx-4 mb-2 flex items-start gap-1.5 text-xs leading-snug">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
            {round.warnings.join("；")}
          </p>
        )}
        <div className="grid gap-0.5 px-1.5 pb-3">
          {round?.refVideo && (
            <Item selected={selection === "ref"} onSelect={() => select("ref")}>
              <Mini>
                <video
                  src={`${round.refVideo.url}#t=0.1`}
                  preload="metadata"
                  muted
                  playsInline
                  className="size-full object-cover"
                />
              </Mini>
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1 text-[13px] font-semibold">
                  <Clapperboard className="size-3.5" />
                  原片
                </span>
                <span className="text-muted-foreground block truncate text-xs">
                  参考拆解
                  {round.reverse && ` · ${round.reverse.shots.length} 个镜头`} ·{" "}
                  {formatSeconds(round.refVideo.duration)}
                </span>
              </span>
              {reverseStep?.status === "running" || reverseStep?.status === "queued" ? (
                <Loader2
                  className="text-status-running size-3.5 animate-spin"
                  aria-label="拆解中"
                />
              ) : reverseStep?.status === "failed" ? (
                <CircleX className="text-destructive size-3.5" aria-label="拆解失败" />
              ) : round.reverseDirty ? (
                <i className="bg-beam size-1.75 rounded-full" title="拆解有改动" />
              ) : null}
            </Item>
          )}

          {round?.status === "scripting" &&
            round.scripts.length === 0 &&
            Array.from({ length: Math.max(round.planCount, 1) }, (_, index) => (
              <div key={index} className="flex items-center gap-2.5 py-2 pr-2.5 pl-3">
                <Mini className="animate-pulse motion-reduce:animate-none" />
                <span className="grid flex-1 gap-1.5">
                  <span className="bg-muted h-3 w-3/5 animate-pulse rounded motion-reduce:animate-none" />
                  <span className="bg-muted h-2.5 w-4/5 animate-pulse rounded motion-reduce:animate-none" />
                </span>
              </div>
            ))}

          {round?.scripts.map((script) => {
            const follow = script.kind === "follow";
            const poster =
              script.status === "done" ? playableTakes(script).at(-1)?.outputs[0]?.posterUrl : "";
            return (
              <Item
                key={script.id}
                selected={selection === script.id}
                onSelect={() => select(script.id)}
              >
                <Mini>
                  {poster ? (
                    <img src={poster} alt="" className="size-full object-cover" />
                  ) : productUrl ? (
                    <img
                      src={productUrl}
                      alt=""
                      className="size-full scale-125 object-cover blur-[6px] saturate-50"
                    />
                  ) : null}
                </Mini>
                <span className="min-w-0 flex-1">
                  <span
                    className={cn(
                      "flex items-center gap-1 text-[13px] font-semibold",
                      follow && "text-beam",
                    )}
                  >
                    {follow && <Copy className="size-3.5" />}
                    {angleName(script.angle, angles)}
                  </span>
                  <span
                    className="text-muted-foreground block truncate text-xs"
                    title={script.title}
                  >
                    {script.title}
                  </span>
                </span>
                <ScriptStatus script={script} />
              </Item>
            );
          })}

          {failedSteps.map((step) => (
            <div
              key={step.id}
              className="border-chrome-border flex items-center gap-2.5 rounded-[18px] border border-dashed py-2 pr-2 pl-3"
            >
              <Mini className="grid place-items-center bg-transparent">
                <FileX className="text-destructive size-4" />
              </Mini>
              <span className="min-w-0 flex-1">
                <span className="block text-[13px] font-semibold">
                  {step.step === "follow" ? "跟拍版" : "改编版"}没写出来
                </span>
                <span className="text-muted-foreground block truncate text-xs" title={step.error}>
                  {step.error || "原因未知"}
                </span>
              </span>
              <button
                type="button"
                aria-label="重试"
                title="重试"
                disabled={actions.busy[`step:${step.id}`]}
                onClick={() => void actions.retryStep(step.id)}
                className="hover:bg-chrome-hover text-muted-foreground hover:text-foreground grid size-8 place-items-center rounded-full disabled:opacity-40"
              >
                <RotateCcw className="size-4" />
              </button>
            </div>
          ))}
        </div>

        <FactsPanel
          project={project}
          scripting={round?.status === "scripting"}
          onEdit={onEditFacts}
        />
      </div>
    </aside>
  );
}
