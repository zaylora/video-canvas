import { useCallback, useState } from "react";
import { useNavigate } from "react-router";
import { motion } from "motion/react";
import { ArrowRight, Combine, Loader2, Play, Plus, RotateCw, Sparkles } from "lucide-react";
import { toast } from "sonner";

import { addEpisode, deleteSegment, listSegments, submitJob } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, isDramaError } from "@/api/drama/errors";
import type { EpisodeSummary } from "@/api/drama/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { VideoPoster } from "@/components/canvas/media-preview";
import { FinalDialog } from "@/components/drama/final-video";
import { DramaPoster } from "@/components/drama/poster";
import { StatusPill } from "@/components/drama/status-pill";
import { VideoModelMenu } from "@/components/drama/video-model-menu";
import { Button } from "@/components/ui/button";
import { useDramaAssets } from "@/hooks/use-drama-assets";
import { useDramaVideoModel } from "@/hooks/use-drama-video-model";
import { DURATION, EASE_OUT, STAGGER, STAGGER_MAX, TAP_CARD } from "@/lib/motion";
import { useDramaJobsStore } from "@/store/drama";
import { useDramaSegmentsStore } from "@/store/drama-segments";
import { activeComposeJob, composeCheckEpisode, finalState } from "@/utils/drama/compose";
import { episodeCardView, storyboardStart } from "@/utils/drama/episode-cards";
import { busyReason } from "@/utils/drama/jobs";
import { countForEpisode, type RefAsset } from "@/utils/drama/segment-assets";

import { useCompose } from "../use-compose";
import { useWorkspace } from "./context";
import { useEstimate } from "./use-estimate";

/** 「x:y」比例转成 CSS aspect-ratio；认不出来用 9:16 */
const posterRatio = (ratio: string) => (/^\d+:\d+$/.test(ratio) ? ratio.replace(":", "/") : "9/16");

/**
 * 生成 / 重新生成一集的分镜脚本：
 * 已有出视频的片段时先确认「会清空已有片段」，确认后逐个删片段再提交；
 * 提交撞上 67013（失败的 take 也算）时同样走确认。
 */
function useStoryboardStart(maxSeconds: number) {
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
function StoryboardButton({
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

function EpisodeCard({
  episode,
  index,
  assets,
  maxSeconds,
  onGoScript,
  starter,
}: {
  episode: EpisodeSummary;
  index: number;
  assets: readonly RefAsset[];
  maxSeconds: number;
  onGoScript: () => void;
  starter: ReturnType<typeof useStoryboardStart>;
}) {
  const { drama, jobs } = useWorkspace();
  const navigate = useNavigate();
  const view = episodeCardView(episode, { busyReason: busyReason(jobs) });
  const counts = countForEpisode(assets, episode.no);
  const { action } = view;
  const busy = starter.pending === episode.no;
  // 合成整集（契约 §14）：成片、是否过期、合成进度
  const final = finalState(episode);
  const composing = activeComposeJob(jobs, episode.no);
  const composeGate = composeCheckEpisode(episode, busyReason(jobs));
  const { compose, pending: composePending } = useCompose(drama.id);
  const [playerOpen, setPlayerOpen] = useState(false);

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      whileTap={TAP_CARD}
      transition={{
        duration: DURATION.base,
        ease: EASE_OUT,
        delay: Math.min(index, STAGGER_MAX) * STAGGER,
      }}
      className="bg-card hover:ring-beam/60 flex gap-3.5 rounded-2xl border p-3 transition-shadow hover:ring-1"
    >
      {episode.final ? (
        <button
          type="button"
          aria-label={`播放第 ${episode.no} 集成片`}
          onClick={() => setPlayerOpen(true)}
          style={{ aspectRatio: posterRatio(drama.ratio) }}
          className="bg-muted focus-visible:ring-ring/50 relative w-[86px] shrink-0 self-start overflow-hidden rounded-[10px] outline-none focus-visible:ring-2"
        >
          <VideoPoster src={episode.final.url} />
          <span className="absolute inset-0 grid place-items-center bg-black/30 text-white">
            <Play className="size-5" />
          </span>
        </button>
      ) : (
        <DramaPoster
          seed={`ep${episode.no}${episode.title}`}
          className="text-foreground/70 grid w-[86px] shrink-0 place-items-center self-start rounded-[10px]"
        >
          <div
            style={{ aspectRatio: posterRatio(drama.ratio) }}
            className="grid w-full place-items-center"
          >
            <Play className="size-5" />
          </div>
        </DramaPoster>
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex items-center gap-1.5">
          <StatusPill tone={view.badge.tone}>
            {action.kind === "generating" && <Loader2 className="animate-spin" />}
            {view.badge.text}
          </StatusPill>
          {composing ? (
            <StatusPill tone="running">
              <Loader2 className="animate-spin" />
              合成中 {composing.progress}%
            </StatusPill>
          ) : final === "fresh" ? (
            <StatusPill tone="success">已合成</StatusPill>
          ) : final === "stale" ? (
            <StatusPill tone="warning">成片已过期</StatusPill>
          ) : null}
        </div>
        <div className="truncate text-sm font-semibold">
          第 {episode.no} 集 · {episode.title}
        </div>
        <div className="text-muted-foreground text-xs tabular-nums">
          角色 {counts.character} · 场景 {counts.scene}
          {episode.segmentCount > 0 &&
            ` · ${episode.segmentCount} 片段 · ${episode.segmentSeconds}s`}
        </div>
        <div className="flex-1" />
        <div className="flex items-center gap-1.5">
          {action.kind === "enter" && (
            <>
              <Button size="sm" onClick={() => navigate(`/drama/${drama.id}/ep/${episode.no}`)}>
                {action.label}
                <ArrowRight />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="重新生成分镜脚本"
                title={busyReason(jobs) ?? "重新生成分镜脚本"}
                disabled={!!busyReason(jobs) || busy}
                onClick={() => void starter.start(episode)}
              >
                <RotateCw />
              </Button>
            </>
          )}
          {action.kind === "storyboard" && (
            <StoryboardButton
              episode={episode}
              label={action.label}
              disabledReason={action.disabledReason}
              maxSeconds={maxSeconds}
              busy={busy}
              onStart={() => void starter.start(episode)}
            />
          )}
          {action.kind === "generating" && (
            <Button variant="outline" size="sm" disabled>
              {action.label}
            </Button>
          )}
          {action.kind === "breakdown" && (
            <Button
              variant="outline"
              size="sm"
              title="先在剧本页对这一集做资产拆解"
              onClick={onGoScript}
            >
              {action.label}
            </Button>
          )}
          {final === "stale" && (
            <Button
              variant="outline"
              size="sm"
              disabled={!composeGate.enabled || !!composing || composePending === episode.no}
              title={composeGate.reason ?? "成片已过期，按当前采用的片段重新合成，不扣积分"}
              onClick={() => void compose(episode.no)}
            >
              {composePending === episode.no ? <Loader2 className="animate-spin" /> : <Combine />}
              重新合成
            </Button>
          )}
        </div>
      </div>
      {episode.final && (
        <FinalDialog
          open={playerOpen}
          onOpenChange={setPlayerOpen}
          asset={episode.final}
          ratio={drama.ratio}
          stale={final === "stale"}
          title={drama.title}
          no={episode.no}
          onRecompose={() => void compose(episode.no)}
          composing={!!composing || composePending === episode.no}
          recomposeReason={composeGate.reason}
        />
      )}
    </motion.div>
  );
}

/**
 * 分集视频页：集卡片网格（状态、角色 / 场景数、片段数与时长、主按钮）。
 * 「生成脚本」带上当前所选视频模型的时长上限作 max_seconds；已有出视频的片段时重跑先确认。
 */
export function EpisodesArea({ onGoScript }: { onGoScript: () => void }) {
  const { drama, refresh } = useWorkspace();
  const settled = useDramaJobsStore((state) => state.settled[drama.id] ?? 0);
  const { assets } = useDramaAssets(drama.canvasId, settled);
  const video = useDramaVideoModel(drama.id);
  const starter = useStoryboardStart(video.maxSeconds);
  const [adding, setAdding] = useState(false);

  const addOne = async () => {
    setAdding(true);
    try {
      const created = await addEpisode(drama.id, { title: "新的一集" });
      toast.success(`已新增第 ${created.no} 集，去剧本页写内容`);
      await refresh();
    } catch (e) {
      toast.error(dramaErrorMessage(e, "新增失败，请稍后重试"));
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="flex flex-wrap items-end gap-2 px-5 pt-6 pb-4">
        <div>
          <h2 className="text-xl font-semibold">分集视频</h2>
          <p className="text-muted-foreground mt-0.5 text-xs tabular-nums">
            共 {drama.episodes.length} 集 · 每集先生成分镜脚本，再逐片段出视频
          </p>
        </div>
        <span className="flex-1" />
        <VideoModelMenu
          models={video.models}
          loading={video.status === "idle" || video.status === "loading"}
          model={video.model}
          onPick={video.setModel}
        />
        <Button variant="outline" size="sm" disabled={adding} onClick={() => void addOne()}>
          {adding ? <Loader2 className="animate-spin" /> : <Plus />}
          新增一集
        </Button>
      </div>
      {drama.episodes.length === 0 ? (
        <p className="text-muted-foreground px-5 py-16 text-center text-sm">
          还没有分集，研读完成后会在这里出现，也可以点右上角手动新增
        </p>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-3.5 px-5 pb-8">
          {drama.episodes.map((episode, index) => (
            <EpisodeCard
              key={episode.no}
              episode={episode}
              index={index}
              assets={assets}
              maxSeconds={video.maxSeconds}
              onGoScript={onGoScript}
              starter={starter}
            />
          ))}
        </div>
      )}
    </div>
  );
}
