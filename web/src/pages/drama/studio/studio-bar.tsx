import { CloudAlert, CloudCheck, Loader2, MonitorPlay, RotateCw } from "lucide-react";

import { ChipPopover, MenuLabel } from "@/components/drama/pickers";
import { VideoModelMenu } from "@/components/drama/video-model-menu";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useDramaJobsStore } from "@/store/drama";
import { busyReason } from "@/utils/drama/jobs";
import { matchOption, paramEntries } from "@/utils/tasks/capabilities";

import { useStoryboardStart } from "../workspace/storyboard-start";
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
 * 片场顶部条：集名、保存状态，右边是视频模型、分辨率、比例和「重新生成分镜脚本」。
 * 合成整集挪到了「成片」页；这里只管把片段做出来。
 */
export function StudioBar() {
  const { drama, episode, no, save, video } = useStudio();
  const jobs = useDramaJobsStore((state) => state.byDrama[drama.id]) ?? NO_JOBS;
  const starter = useStoryboardStart(video.maxSeconds);
  const status = SAVE_LABEL[save];
  const blocked = busyReason(jobs);
  return (
    <div className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <h2 className="font-display min-w-0 truncate text-[17px] font-bold tracking-wide">
        第 {no} 集 · {episode.title}
      </h2>
      <span
        role="status"
        className={cn(
          "text-muted-foreground inline-flex items-center gap-1 text-xs max-sm:hidden",
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
        <span className="text-muted-foreground px-2 font-mono text-xs">
          {drama.ratio || "比例未定"}
        </span>
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="重新生成分镜脚本"
        title={blocked ?? "重新生成分镜脚本（已有出视频的片段会先确认）"}
        disabled={!!blocked || starter.pending === no}
        onClick={() => void starter.start(episode)}
      >
        {starter.pending === no ? <Loader2 className="animate-spin" /> : <RotateCw />}
      </Button>
    </div>
  );
}
