import { Loader2 } from "lucide-react";

import { VideoModelMenu } from "@/components/drama/video-model-menu";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { hasVideo } from "@/utils/drama/segments";

import { useStudio } from "./use-studio";

/** 为什么现在不能生成；能生成为 null（写进按钮的悬停提示） */
function useBlockedReason(): string | null {
  const { selected, plan, video, submitting } = useStudio();
  if (!selected || !plan) return "还没有片段";
  if (selected.status === "running") return "这个片段正在生成";
  if (submitting.has(selected.id)) return "正在提交";
  if (video.status === "idle" || video.status === "loading") return "正在加载模型清单";
  if (!video.model) return "没有可用的视频模型（需要支持参考图）";
  return Object.values(plan.errors)[0] ?? null;
}

/**
 * 片场顶栏右侧的生成操作：视频模型选择 + 「立即生成 · 积分」。
 * 生成的是当前选中的片段；积分不够时数字标红，原因写在悬停提示里。
 */
export function GenerateControls() {
  const { selected, credits, availableCredits, actions, submitting, video } = useStudio();
  const blocked = useBlockedReason();
  const busy = !!selected && (submitting.has(selected.id) || selected.status === "running");
  const out = !!selected && hasVideo(selected);
  const lowCredits =
    credits !== undefined && availableCredits !== null && credits > availableCredits;

  return (
    <div className="flex items-center gap-1.5">
      <VideoModelMenu
        models={video.models}
        loading={video.status === "idle" || video.status === "loading"}
        model={video.model}
        onPick={video.setModel}
      />
      <Button
        size="sm"
        disabled={!!blocked}
        title={
          blocked ??
          (lowCredits
            ? `积分不足：本次约需 ${credits}，当前可用 ${availableCredits}`
            : "开始生成这个片段的视频（⌘↵）")
        }
        onClick={() => selected && void actions.generate(selected)}
      >
        {busy && <Loader2 className="animate-spin" />}
        {out ? "再生成一次" : "立即生成"}
        {credits !== undefined && (
          <>
            <span aria-hidden className="opacity-50">
              ·
            </span>
            <span className={cn("tabular-nums", lowCredits && "text-destructive")}>{credits}</span>
          </>
        )}
      </Button>
    </div>
  );
}
