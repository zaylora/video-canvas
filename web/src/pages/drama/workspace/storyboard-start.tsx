import { useCallback, useState } from "react";
import { Loader2, Sparkles } from "lucide-react";
import { toast } from "sonner";

import { deleteSegment, listSegments, submitJob } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, isDramaError } from "@/api/drama/errors";
import type { EpisodeSummary } from "@/api/drama/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { Button } from "@/components/ui/button";
import { useDramaJobsStore } from "@/store/drama";
import { useDramaSegmentsStore } from "@/store/drama-segments";
import { storyboardStart } from "@/utils/drama/episode-cards";

import { useWorkspace } from "./context";
import { useEstimate } from "./use-estimate";

/**
 * 生成 / 重新生成一集的分镜脚本：
 * 已有出视频的片段时先确认「会清空已有片段」，确认后逐个删片段再提交；
 * 提交撞上 67013（失败的 take 也算）时同样走确认。
 */
export function useStoryboardStart(maxSeconds: number) {
  const { drama, refresh } = useWorkspace();
  const [pending, setPending] = useState<number | null>(null);

  const submit = useCallback(
    async (no: number) => {
      const job = await submitJob(drama.id, { step: "storyboard", episodes: [no], maxSeconds });
      useDramaJobsStore.getState().applyJob(drama.id, job);
      void refresh();
    },
    [drama.id, maxSeconds, refresh],
  );

  const clearThenSubmit = useCallback(
    async (no: number) => {
      for (const segment of await listSegments(drama.id, no, false)) {
        await deleteSegment(segment.id);
      }
      useDramaSegmentsStore.getState().setList(drama.id, no, []);
      await submit(no);
    },
    [drama.id, submit],
  );

  const askClear = useCallback(
    (no: number) =>
      confirm({
        title: `重新生成第 ${no} 集的分镜脚本？`,
        description:
          "这一集已经有生成结果，重新生成会清空已有片段（片段提示词和采用状态）。已生成的视频文件不会被删除。",
        confirmLabel: "清空并重新生成",
        destructive: true,
        onConfirm: () => clearThenSubmit(no),
      }),
    [clearThenSubmit],
  );

  const start = useCallback(
    async (episode: EpisodeSummary) => {
      if (storyboardStart(episode).confirm) {
        await askClear(episode.no);
        return;
      }
      setPending(episode.no);
      try {
        await submit(episode.no);
      } catch (e) {
        if (isDramaError(e, DRAMA_ERR.storyboardHasTakes)) await askClear(episode.no);
        else toast.error(dramaErrorMessage(e, "提交失败，请稍后重试"));
      } finally {
        setPending(null);
      }
    },
    [askClear, submit],
  );

  return { start, pending };
}

/** 「生成脚本 · n 积分」主按钮：估价随所选视频模型的时长上限变化 */
export function StoryboardButton({
  episode,
  label,
  disabledReason,
  maxSeconds,
  busy,
  onStart,
}: {
  episode: EpisodeSummary;
  label: string;
  disabledReason: string | null;
  maxSeconds: number;
  busy: boolean;
  onStart: () => void;
}) {
  const { drama } = useWorkspace();
  const estimate = useEstimate(drama.id, {
    step: "storyboard",
    episodes: [episode.no],
    maxSeconds,
  });
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={!!disabledReason || busy}
      title={disabledReason ?? (estimate.status === "error" ? estimate.message : undefined)}
      onClick={onStart}
    >
      {busy ? <Loader2 className="animate-spin" /> : <Sparkles />}
      {label}
      {estimate.status === "ready" && (
        <>
          <span aria-hidden>·</span>
          <span className="text-credit tabular-nums">{estimate.credits}</span>
        </>
      )}
    </Button>
  );
}
