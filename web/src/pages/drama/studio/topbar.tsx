import { Link } from "react-router";
import { ChevronLeft, CloudAlert, CloudCheck, Combine, Loader2, MonitorPlay } from "lucide-react";

import { ChipPopover, MenuLabel } from "@/components/drama/pickers";
import { VideoModelMenu } from "@/components/drama/video-model-menu";
import { StatusPill } from "@/components/drama/status-pill";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { CreditsPill } from "@/pages/canvas/chrome/top-right-bar";
import { matchOption, paramEntries } from "@/utils/tasks/capabilities";

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

/** 片段工作区顶栏：返回分集、集名、保存状态、视频模型、分辨率 / 比例、积分、合成整集（三期） */
export function Topbar() {
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
        size="sm"
        disabled
        title="合成整集在三期上线：服务端按片段顺序拼接"
        className="ml-1 max-sm:hidden"
      >
        <Combine />
        合成整集
        <StatusPill tone="muted" className="h-4 text-[10px]">
          三期
        </StatusPill>
      </Button>
    </header>
  );
}
