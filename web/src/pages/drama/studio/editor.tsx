import { motion } from "motion/react";
import {
  AlertTriangle,
  ChevronDown,
  CircleX,
  Image as ImageIcon,
  Loader2,
  Sparkles,
  Trash2,
} from "lucide-react";

import { ChipPopover, MenuLabel } from "@/components/drama/pickers";
import { StatusPill } from "@/components/drama/status-pill";
import { Button } from "@/components/ui/button";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { modelMaxSeconds } from "@/utils/drama/video-models";
import { hasVideo, latestError } from "@/utils/drama/segments";
import { Link } from "react-router";

import { PromptPane } from "./prompt-pane";
import { RefsBar } from "./refs-bar";
import { ShotsTable } from "./shots-table";
import { useStudio } from "./use-studio";

const SECOND_CHOICES = [3, 4, 5, 6, 8, 10, 12, 15, 20, 30];

/** 片段时长：选项不超过所选模型的时长上限，当前值总在里面 */
function SecondsChip() {
  const { selected, actions, video } = useStudio();
  if (!selected) return null;
  const max = modelMaxSeconds(video.model?.capabilities) ?? 15;
  const options = [...new Set([...SECOND_CHOICES.filter((s) => s <= max), selected.seconds])].sort(
    (a, b) => a - b,
  );
  return (
    <ChipPopover
      variant="bar"
      icon={<span className="sr-only">时长</span>}
      label={`${selected.seconds}s`}
      title={`片段时长 · 模型上限 ${max}s`}
      width="w-40"
    >
      {(close) => (
        <>
          <MenuLabel>片段时长 · 模型上限 {max}s</MenuLabel>
          {options.map((seconds) => (
            <button
              key={seconds}
              type="button"
              onClick={() => {
                close();
                void actions.setSeconds(selected, seconds);
              }}
              className={cn(
                "hover:bg-muted focus-visible:ring-ring/50 flex min-h-8 items-center rounded-md px-2 text-left text-sm tabular-nums outline-none focus-visible:ring-2",
                seconds === selected.seconds && "bg-muted font-medium",
                seconds > max && "text-status-warning",
              )}
            >
              {seconds}s{seconds > max && <span className="ml-auto text-[11px]">超出模型上限</span>}
            </button>
          ))}
        </>
      )}
    </ChipPopover>
  );
}

/** 首帧：生成按钮 + 图片模型选择；生成后这个片段改走图生视频 */
function KeyframeButton() {
  const { selected, image, keyframeCredits, actions, submitting } = useStudio();
  if (!selected) return null;
  const busy = submitting.has(selected.id);
  return (
    <div className="flex items-center">
      <Button
        variant="outline"
        size="sm"
        disabled={busy || !image.model}
        title={image.model ? `用 ${image.model.label} 生成这个片段的首帧图` : "没有可用的图片模型"}
        className="rounded-r-none"
        onClick={() => void actions.keyframe(selected)}
      >
        {selected.keyframe ? (
          <img
            src={selected.keyframe.url}
            alt=""
            draggable={false}
            className="size-5 rounded-[4px] object-cover"
          />
        ) : (
          <ImageIcon />
        )}
        {selected.keyframe ? "首帧已生成 · 图生视频" : "生成首帧（可选）"}
        {keyframeCredits !== undefined && (
          <span className="text-credit text-xs tabular-nums">{keyframeCredits}</span>
        )}
      </Button>
      <ChipPopover
        variant="bar"
        icon={<ChevronDown />}
        label=""
        title="首帧用的图片模型"
        width="w-64"
      >
        {(close) => (
          <>
            <MenuLabel>首帧图片模型</MenuLabel>
            {image.models.map((info) => (
              <button
                key={info.key}
                type="button"
                onClick={() => {
                  close();
                  image.setModel(info.key);
                }}
                className={cn(
                  "hover:bg-muted flex min-h-8 items-center rounded-md px-2 text-left text-sm",
                  image.model?.key === info.key && "bg-muted font-medium",
                )}
              >
                <span className="truncate">{info.label}</span>
              </button>
            ))}
            {image.models.length === 0 && (
              <div className="text-muted-foreground px-2 py-3 text-sm">暂无可用的图片模型</div>
            )}
          </>
        )}
      </ChipPopover>
    </div>
  );
}

/** 为什么现在不能生成；能生成为 null（按钮灰着时写给用户看） */
function useBlockedReason(): string | null {
  const { selected, plan, video, submitting } = useStudio();
  if (!selected || !plan) return "还没有片段";
  if (selected.status === "running") return "这个片段正在生成";
  if (submitting.has(selected.id)) return "正在提交";
  if (video.status === "idle" || video.status === "loading") return "正在加载模型清单";
  if (!video.model) return "没有可用的视频模型（需要支持参考图）";
  return Object.values(plan.errors)[0] ?? null;
}

/** 中间编辑区：片段标题与时长、参考素材条、提示词、首帧、积分与生成、失败提示、分镜表 */
export function Editor() {
  const {
    selected,
    plan,
    credits,
    availableCredits,
    actions,
    submitting,
    segmentsStatus,
    drama,
    no,
  } = useStudio();
  const blocked = useBlockedReason();

  if (!selected) {
    return (
      <section className="min-w-0 flex-1 overflow-y-auto">
        <p className="text-muted-foreground mt-20 text-center text-sm">
          {segmentsStatus === "loading"
            ? "加载中…"
            : segmentsStatus === "error"
              ? "片段读取失败，请刷新页面重试"
              : "这一集还没有片段，点下方「+」新建，或回到分集页生成分镜脚本"}
        </p>
        {segmentsStatus === "ready" && (
          <p className="text-center">
            <Link to={`/drama/${drama.id}?area=episodes`} className="text-sm underline">
              回到分集视频
            </Link>
          </p>
        )}
      </section>
    );
  }

  const error = latestError(selected);
  const out = hasVideo(selected);
  const busy = submitting.has(selected.id) || selected.status === "running";
  const lowCredits =
    credits !== undefined && availableCredits !== null && credits > availableCredits;

  return (
    <section
      aria-label={`第 ${no} 集 片段 ${selected.seq + 1}`}
      className="min-w-0 flex-1 overflow-y-auto"
    >
      <motion.div
        key={selected.id}
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        className="mx-auto grid max-w-[760px] gap-4 px-7 pt-5 pb-10"
      >
        <div className="flex items-baseline gap-2">
          <h2 className="text-[22px] font-semibold tabular-nums">
            片段 {String(selected.seq + 1).padStart(2, "0")}
          </h2>
          <SecondsChip />
          <span className="flex-1" />
          {selected.status === "error" && <StatusPill tone="destructive">生成失败</StatusPill>}
          {selected.status === "running" && (
            <StatusPill tone="running">
              <Loader2 className="animate-spin" />
              生成中
            </StatusPill>
          )}
          {selected.status !== "running" && selected.status !== "error" && out && (
            <StatusPill tone="success">已出视频</StatusPill>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="删除这个片段"
            title="删除这个片段"
            onClick={() => void actions.remove(selected)}
          >
            <Trash2 />
          </Button>
        </div>

        <RefsBar />

        <PromptPane key={selected.id} segment={selected} />

        <div className="flex flex-wrap items-center gap-2">
          <KeyframeButton />
          <span className="flex-1" />
          <span
            className="text-muted-foreground text-xs tabular-nums"
            title={availableCredits !== null ? `当前可用 ${availableCredits} 积分` : undefined}
          >
            {credits !== undefined ? (
              <>
                约{" "}
                <span className={cn("text-credit", lowCredits && "text-destructive")}>
                  {credits}
                </span>{" "}
                积分
              </>
            ) : (
              "—"
            )}
          </span>
          <Button
            disabled={!!blocked}
            title={blocked ?? "开始生成这个片段的视频"}
            onClick={() => void actions.generate(selected)}
          >
            {busy ? <Loader2 className="animate-spin" /> : <Sparkles />}
            {out ? "再生成一次" : "立即生成"}
            <kbd className="border-primary-foreground/30 rounded border px-1 text-[10px] font-normal">
              ⌘↵
            </kbd>
          </Button>
        </div>
        {blocked && selected.status !== "running" && !submitting.has(selected.id) && (
          <p className="text-muted-foreground -mt-2 text-right text-xs">{blocked}</p>
        )}
        {lowCredits && (
          <p className="text-destructive -mt-2 text-right text-xs">
            积分不足：本次约需 {credits}，当前可用 {availableCredits}
          </p>
        )}

        {plan && plan.deadLabels.length > 0 && (
          <div className="bg-status-warning/10 flex items-start gap-2 rounded-lg px-3 py-2 text-sm">
            <AlertTriangle className="text-status-warning mt-0.5 size-4 shrink-0" />
            <div>
              「{plan.deadLabels.join("」「")}
              」对应的资产已被删除。可以继续生成，这部分只按文字提交。
            </div>
          </div>
        )}
        {error && (
          <div
            role="alert"
            className="bg-destructive/8 border-destructive/25 flex items-start gap-2 rounded-lg border px-3 py-2 text-sm"
          >
            <CircleX className="text-destructive mt-0.5 size-4 shrink-0" />
            <div>{error}。改一下提示词再试，失败不扣积分。</div>
          </div>
        )}

        <ShotsTable shots={selected.shots} />
      </motion.div>
    </section>
  );
}
