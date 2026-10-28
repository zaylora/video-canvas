import { Link } from "react-router";
import {
  Bot,
  ChevronLeft,
  CloudAlert,
  CloudCheck,
  Combine,
  Loader2,
  MonitorPlay,
} from "lucide-react";

import { ChipPopover, MenuLabel } from "@/components/drama/pickers";
import { VideoModelMenu } from "@/components/drama/video-model-menu";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { CreditsPill } from "@/pages/canvas/chrome/top-right-bar";
import { useDramaJobsStore } from "@/store/drama";
import {
  activeComposeJob,
  composeCheck,
  finalState,
  missingVideoSeqs,
  skippedText,
} from "@/utils/drama/compose";
import { busyReason } from "@/utils/drama/jobs";
import { matchOption, paramEntries } from "@/utils/tasks/capabilities";

import { useCompose } from "../use-compose";
import type { SaveSummary } from "../workspace/use-workspace";
import { useStudio } from "./use-studio";

const SAVE_LABEL: Record<SaveSummary, { text: string; icon: React.ReactNode; tone?: string }> = {
  saved: { text: "已保存", icon: <CloudCheck className="size-3.5" /> },
  saving: { text: "保存中", icon: <Loader2 className="size-3.5 animate-spin" /> },
  error: {
    text: "保存失败，将重试",
    icon: <CloudAlert className="size-3.5" />,
    tone: "text-destructive",
  },
  conflict: {
    text: "有冲突，已停止保存",
    icon: <CloudAlert className="size-3.5" />,
    tone: "text-status-warning",
  },
};

/** 分辨率：模型有开放的分辨率参数才显示；选择存在本机，按短剧分开 */
function ResolutionChip() {
  const { video, overrides, setOverride } = useStudio();
  const field = paramEntries(video.model?.capabilities).find(
    (entry) => entry.open && entry.type === "enum" && entry.name === "resolution",
  );
  if (!field) return null;
  const current = overrides[field.name] ?? field.default;
  const hit = matchOption(field, current);
  return (
    <ChipPopover
      variant="bar"
      icon={<MonitorPlay />}
      label={String(hit ?? field.default ?? "分辨率")}
    >
      {(close) => (
        <>
          <MenuLabel>{field.label}</MenuLabel>
          {(field.options ?? []).map((option) => (
            <button
              key={String(option)}
              type="button"
              onClick={() => {
                close();
                setOverride(field.name, option);
              }}
              className={cn(
                "hover:bg-muted focus-visible:ring-ring/50 flex min-h-8 items-center rounded-md px-2 text-left text-sm outline-none focus-visible:ring-2",
                String(option) === String(hit) && "bg-muted font-medium",
              )}
            >
              {String(option)}
            </button>
          ))}
        </>
      )}
    </ChipPopover>
  );
}

const NO_JOBS: never[] = [];

/**
 * 合成整集：至少一个片段有采用视频才可点（否则写明原因）；服务端 ffmpeg 拼接，不扣积分；
 * 合成中按钮显示进度；已有成片时叫「重新合成」，成片过期时按钮更醒目。
 */
function ComposeButton() {
  const { drama, episode, no, segments, segmentsStatus } = useStudio();
  const jobs = useDramaJobsStore((state) => state.byDrama[drama.id]) ?? NO_JOBS;
  const { compose, pending } = useCompose(drama.id);
  const running = activeComposeJob(jobs, no);
  const check = composeCheck(segments, busyReason(jobs));
  const state = finalState(episode);
  const skipped = missingVideoSeqs(segments);
  const loading = segmentsStatus === "loading";
  const disabled = loading || !check.enabled || !!running || pending === no;
  const title = running
    ? "合成中"
    : loading
      ? "片段加载中"
      : (check.reason ??
        `按顺序拼接各片段采用的视频，不扣积分${
          skipped.length > 0 ? `；没有采用视频的片段（${skippedText(skipped)}）会被跳过` : ""
        }${state === "stale" ? "；成片已过期" : ""}`);
  return (
    <Button
      size="sm"
      variant={state === "fresh" ? "outline" : "default"}
      disabled={disabled}
      title={title}
      onClick={() => void compose(no)}
      className="ml-1 max-sm:hidden"
    >
      {running || pending === no ? <Loader2 className="animate-spin" /> : <Combine />}
      {running ? `合成中 ${running.progress}%` : state === "none" ? "合成整集" : "重新合成"}
      {state === "stale" && !running && (
        <span className="bg-status-warning size-1.5 rounded-full" aria-label="成片已过期" />
      )}
    </Button>
  );
}

/** 片段工作区顶栏：返回分集、集名、保存状态、视频模型、分辨率 / 比例、积分、助手、合成整集 */
export function Topbar({
  assistantOpen,
  onToggleAssistant,
}: {
  assistantOpen: boolean;
  onToggleAssistant: () => void;
}) {
  const { drama, episode, no, save, video } = useStudio();
  const status = SAVE_LABEL[save];
  return (
    <header className="border-border flex h-12 shrink-0 items-center gap-1 border-b px-3">
      <Button
        variant="ghost"
        size="sm"
        nativeButton={false}
        render={<Link to={`/drama/${drama.id}?area=episodes`} />}
      >
        <ChevronLeft />
        分集视频
      </Button>
      <span className="text-muted-foreground">/</span>
      <b className="min-w-0 truncate px-1.5 text-sm">
        第 {no} 集 · {episode.title}
      </b>
      <span
        role="status"
        className={cn(
          "text-muted-foreground ml-1 inline-flex items-center gap-1 text-xs max-sm:hidden",
          status.tone,
        )}
      >
        {status.icon}
        {status.text}
      </span>
      <span className="flex-1" />
      <div className="flex items-center gap-0.5 max-md:hidden">
        <VideoModelMenu
          models={video.models}
          loading={video.status === "idle" || video.status === "loading"}
          model={video.model}
          onPick={video.setModel}
        />
        <ResolutionChip />
        <span className="text-muted-foreground px-2 text-xs tabular-nums">
          {drama.ratio || "比例未定"}
        </span>
      </div>
      <ThemeToggle />
      <CreditsPill />
      <Button
        variant={assistantOpen ? "secondary" : "ghost"}
        size="sm"
        aria-pressed={assistantOpen}
        title="短剧助手：对话修改剧本、资产和分镜"
        onClick={onToggleAssistant}
      >
        <Bot />
        助手
      </Button>
      <ComposeButton />
    </header>
  );
}
