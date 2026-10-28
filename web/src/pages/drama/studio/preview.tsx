import { CircleX, Clapperboard, Loader2 } from "lucide-react";

import { VideoPoster } from "@/components/canvas/media-preview";
import { FinalVideo } from "@/components/drama/final-video";
import { SHIMMER } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDramaJobsStore } from "@/store/drama";
import { activeComposeJob, composeCheck, finalState } from "@/utils/drama/compose";
import { busyReason } from "@/utils/drama/jobs";
import { playableTake } from "@/utils/drama/segments";

import { useCompose } from "../use-compose";
import { usePlaybackContext } from "./use-playback";
import { useStudio } from "./use-studio";

/** 「x:y」比例转成 CSS aspect-ratio；认不出来用 9:16 */
const aspectOf = (ratio: string) => (/^\d+:\d+$/.test(ratio) ? ratio.replace(":", "/") : "9/16");

const NO_JOBS: never[] = [];

/** 右预览里的成片：有成片时显示播放器、下载，过期时提示并给出重新合成 */
function FinalSection() {
  const { drama, episode, no, segments } = useStudio();
  const jobs = useDramaJobsStore((state) => state.byDrama[drama.id]) ?? NO_JOBS;
  const { compose, pending } = useCompose(drama.id);
  const state = finalState(episode);
  if (!episode.final || state === "none") return null;
  const running = activeComposeJob(jobs, no);
  const check = composeCheck(segments, busyReason(jobs));
  return (
    <section aria-label="成片" className="border-border border-b pb-3">
      <div className="mb-1.5 flex items-center gap-2 text-sm font-medium">
        成片
        {running && (
          <span className="text-status-running inline-flex items-center gap-1 text-xs font-normal">
            <Loader2 className="size-3 animate-spin" />
            合成中 {running.progress}%
          </span>
        )}
      </div>
      <FinalVideo
        asset={episode.final}
        ratio={drama.ratio}
        stale={state === "stale"}
        title={drama.title}
        no={no}
        onRecompose={() => void compose(no)}
        composing={pending === no || !!running}
        recomposeReason={check.reason}
      />
    </section>
  );
}

/** 右预览：采用的视频播放；连播时显示当前片段（两个 video 双缓冲）；多次结果的缩略图，点选即采用 */
export function Preview() {
  const { selected, drama, actions, submitting, segments } = useStudio();
  const playback = usePlaybackContext();
  // 连播时预览跟着当前片段（选中也会跟过去，这里直接按连播的当前片段取，不等选中更新）
  const current = segments.find((item) => item.id === playback.currentId) ?? selected;
  const take = selected ? playableTake(selected) : null;
  const running = !!selected && (selected.status === "running" || submitting.has(selected.id));
  return (
    <aside
      aria-label="预览"
      className="border-border flex w-[300px] shrink-0 flex-col gap-3 border-l p-4 max-lg:hidden"
    >
      <FinalSection />
      <div className="flex items-center gap-2">
        <b className="text-sm">预览</b>
        {playback.active && (
          <span className="bg-beam/15 text-beam inline-flex h-4 items-center rounded-full px-2 text-[10px] font-medium">
            连播{playback.playing ? "中" : "已暂停"}
          </span>
        )}
      </div>
      <div
        style={{ aspectRatio: aspectOf(drama.ratio) }}
        className="bg-muted relative mx-auto grid max-h-[420px] w-full place-items-center overflow-hidden rounded-[14px]"
      >
        {/* 连播的两个 video 一直挂着（下一段在不可见的那个里预加载），只显示当前画面；无连播时都是透明的 */}
        {([0, 1] as const).map((which) => {
          const slot = playback.slot(which);
          return (
            <video
              key={which}
              ref={slot.ref}
              playsInline
              preload="auto"
              aria-hidden={!(playback.active && slot.visible)}
              onClick={playback.toggle}
              onEnded={slot.onEnded}
              onError={slot.onError}
              onTimeUpdate={slot.onTimeUpdate}
              className={cn(
                "absolute inset-0 size-full bg-black object-contain",
                !(playback.active && slot.visible) && "pointer-events-none opacity-0",
              )}
            />
          );
        })}
        {playback.active ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 flex justify-between bg-black/55 px-2.5 py-1 text-[11px] text-white tabular-nums">
            <span>片段 {String((current?.seq ?? 0) + 1).padStart(2, "0")}</span>
            <span>{current?.seconds}s</span>
          </div>
        ) : take?.asset ? (
          <video
            key={take.taskId}
            src={take.asset.url}
            controls
            playsInline
            preload="metadata"
            className="size-full bg-black object-contain"
          />
        ) : running ? (
          <div className="text-muted-foreground text-center text-xs">
            <Loader2 className="mx-auto mb-2 size-5 animate-spin" />
            <span
              className="inline-block animate-pulse motion-reduce:animate-none"
              style={{ animationDuration: `${SHIMMER}s` }}
            >
              生成中，通常要几分钟
            </span>
          </div>
        ) : (
          <div className="text-muted-foreground text-center text-xs">
            <Clapperboard className="mx-auto mb-1.5 size-5" />
            还没有生成内容
          </div>
        )}
      </div>
      {selected && selected.takes.length > 0 && (
        <div>
          <div className="text-muted-foreground mb-1.5 text-[11.5px]">生成结果 · 点选即采用</div>
          <div className="flex flex-wrap gap-2">
            {selected.takes.map((item, index) => {
              const chosen = selected.chosenTaskId === item.taskId;
              const ok = item.status === "succeeded" && !!item.asset;
              const ing =
                !ok &&
                item.status !== "failed" &&
                item.status !== "canceled" &&
                item.status !== "expired";
              return (
                <button
                  key={item.taskId}
                  type="button"
                  aria-pressed={chosen}
                  aria-label={`第 ${index + 1} 次结果${chosen ? "（已采用）" : ""}`}
                  disabled={!ok}
                  title={
                    ok ? "采用这一条" : item.errorMessage || (ing ? "生成中" : "没有可用的结果")
                  }
                  onClick={() => void actions.choose(selected, item.taskId)}
                  style={{ aspectRatio: aspectOf(drama.ratio) }}
                  className={cn(
                    "bg-muted focus-visible:ring-ring/50 relative w-[52px] overflow-hidden rounded-lg border-2 outline-none focus-visible:ring-2",
                    chosen ? "border-foreground" : "border-transparent",
                    !ok && "cursor-not-allowed opacity-70",
                  )}
                >
                  {ok && item.asset ? (
                    <VideoPoster src={item.asset.url} />
                  ) : ing ? (
                    <Loader2 className="text-muted-foreground m-auto size-4 animate-spin" />
                  ) : (
                    <CircleX className="text-destructive m-auto size-4" />
                  )}
                </button>
              );
            })}
          </div>
        </div>
      )}
    </aside>
  );
}
