import { useEffect, useState } from "react";
import { Check, Clapperboard, Combine, Download, Film, Loader2, TriangleAlert } from "lucide-react";

import { VideoPoster } from "@/components/canvas/media-preview";
import { finalFileName } from "@/components/drama/final-file";
import { Button } from "@/components/ui/button";
import { VideoPlayer } from "@/components/video-player";
import { cn } from "@/lib/utils";
import { segmentKey, useDramaSegmentsStore } from "@/store/drama-segments";
import { downloadMedia } from "@/utils/canvas/download";
import {
  activeComposeJob,
  composeCheck,
  finalState,
  missingVideoSeqs,
  skippedText,
} from "@/utils/drama/compose";
import { busyReason } from "@/utils/drama/jobs";
import { reloadSegments } from "@/utils/drama/segment-events";
import { playableTake } from "@/utils/drama/segments";
import type { WorkspaceStage } from "@/utils/drama/stages";

import { useCompose } from "../use-compose";
import { useWorkspace } from "./context";

/** 「x:y」比例转成 CSS aspect-ratio；认不出来用 9:16 */
const aspectOf = (ratio: string) => (/^\d+:\d+$/.test(ratio) ? ratio.replace(":", "/") : "9/16");

const NO_SEGMENTS: never[] = [];

const mmss = (seconds: number) =>
  `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(Math.floor(seconds % 60)).padStart(2, "0")}`;

/**
 * 成片：左边播放当前集的成片，右边是合成面板（已采用片段数、成片时长、片段清单、合成 / 重新合成）。
 * 合成在服务端用 ffmpeg 按顺序拼接各片段采用的视频，不扣积分；没有采用视频的片段会被跳过。
 */
export function FinalArea({
  ep,
  onGo,
}: {
  ep: number;
  onGo: (stage: WorkspaceStage, ep?: number) => void;
}) {
  const { drama, jobs } = useWorkspace();
  const episode = drama.episodes.find((item) => item.no === ep) ?? drama.episodes[0];
  const no = episode?.no ?? 0;
  const segments =
    useDramaSegmentsStore((state) => state.byEpisode[segmentKey(drama.id, no)]) ?? NO_SEGMENTS;
  const { compose, pending } = useCompose(drama.id);
  const [saving, setSaving] = useState(false);

  // 片段读自片段接口：进到成片页（或换集）时拉一次，之后靠推送保持新鲜
  useEffect(() => {
    if (no > 0) void reloadSegments(drama.id, no);
  }, [drama.id, no]);

  if (!episode) {
    return (
      <p className="text-muted-foreground grid h-full place-items-center text-sm">还没有分集</p>
    );
  }

  const final = episode.final;
  const state = finalState(episode);
  const running = activeComposeJob(jobs, no);
  const check = composeCheck(segments, busyReason(jobs));
  const skipped = missingVideoSeqs(segments);
  const adopted = segments.filter((segment) => playableTake(segment)).length;
  const seconds = segments.reduce(
    (sum, segment) => sum + (playableTake(segment) ? segment.seconds : 0),
    0,
  );
  const composing = !!running || pending === no;
  const withFinal = drama.episodes.filter((item) => item.final);

  const download = async () => {
    if (!final) return;
    setSaving(true);
    try {
      await downloadMedia(final.url, finalFileName(drama.title, no));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex h-full min-h-0 max-lg:flex-col">
      <section className="relative flex min-w-0 flex-1 flex-col items-center gap-4 overflow-y-auto px-6 py-5">
        <div className="flex w-full items-center gap-2.5">
          <h2 className="font-display text-[17px] font-bold tracking-wide">
            第 {no} 集 · {episode.title}
          </h2>
          {final && (
            <span
              className={cn(
                "rounded-md px-2 py-0.5 text-[11px] font-medium",
                state === "stale"
                  ? "bg-primary/15 text-primary"
                  : "bg-status-success/15 text-status-success",
              )}
            >
              {state === "stale" ? "成片已过期，可重新合成" : "成片已合成"}
            </span>
          )}
          <span className="flex-1" />
          {final && (
            <Button variant="outline" size="sm" disabled={saving} onClick={() => void download()}>
              {saving ? <Loader2 className="animate-spin" /> : <Download />}
              下载 mp4
            </Button>
          )}
        </div>

        {final ? (
          // 和画布视频节点同一个播放器：先显示封面，点了才加载视频
          <div
            style={{ aspectRatio: aspectOf(drama.ratio) }}
            className="relative h-[66vh] max-w-full shrink-0 overflow-hidden rounded-2xl bg-black shadow-[0_40px_80px_-30px_rgb(0_0_0/0.9)] ring-1 ring-white/10"
          >
            <VideoPlayer key={final.id} src={final.url} durationMs={final.durationMs} />
          </div>
        ) : (
          <div className="flex flex-col items-center gap-3 py-14 text-center">
            <span className="bg-muted text-muted-foreground grid size-16 place-items-center rounded-[20px]">
              <Film className="size-7" aria-hidden />
            </span>
            <h3 className="text-[17px] font-semibold">第 {no} 集还没有成片</h3>
            <p className="text-muted-foreground max-w-[40ch] text-[13px]">
              {episode.segmentCount > 0
                ? `${episode.doneSegmentCount}/${episode.segmentCount} 个片段已有视频。采用每个片段的视频后，在右侧合成整集。`
                : "先生成分镜脚本、再逐个片段出视频，最后在这里合成整集。"}
            </p>
            <Button variant="outline" onClick={() => onGo("studio", no)}>
              <Clapperboard />
              去片场
            </Button>
          </div>
        )}

        {withFinal.length > 1 && (
          <div className="w-full">
            <div className="text-muted-foreground mb-2 text-xs">其他集成片</div>
            <div className="flex gap-2.5 overflow-x-auto pb-1">
              {withFinal.map((item) => (
                <button
                  key={item.no}
                  type="button"
                  aria-label={`第 ${item.no} 集成片`}
                  aria-current={item.no === no}
                  onClick={() => onGo("final", item.no)}
                  style={{ aspectRatio: aspectOf(drama.ratio) }}
                  className={cn(
                    "bg-muted focus-visible:ring-ring/50 relative w-16 shrink-0 overflow-hidden rounded-[9px] border-2 outline-none focus-visible:ring-2",
                    item.no === no ? "border-primary" : "hover:border-border border-transparent",
                  )}
                >
                  {item.final && <VideoPoster src={item.final.url} />}
                  <span className="absolute bottom-1 left-1.5 font-mono text-[11px] font-semibold text-white [text-shadow:0_1px_4px_rgb(0_0_0/0.8)]">
                    EP{String(item.no).padStart(2, "0")}
                  </span>
                </button>
              ))}
            </div>
          </div>
        )}
      </section>

      <aside
        aria-label="合成整集"
        className="border-border bg-card flex w-[360px] shrink-0 flex-col border-l max-lg:w-full max-lg:border-t max-lg:border-l-0"
      >
        <div className="px-4 pt-4 pb-2">
          <h3 className="text-[15px] font-semibold">合成整集</h3>
          <p className="text-muted-foreground mt-0.5 text-xs">
            按片段顺序拼接采用的版本，统一到项目比例 · 不扣积分
          </p>
        </div>
        <div className="grid min-h-0 flex-1 content-start gap-3.5 overflow-y-auto px-4 pb-4">
          <div className="grid grid-cols-3 gap-2">
            {[
              [`${adopted}/${segments.length}`, "已采用片段"],
              [mmss(seconds), "成片时长"],
              [state === "none" ? "—" : state === "stale" ? "过期" : "最新", "成片状态"],
            ].map(([value, label]) => (
              <div key={label} className="bg-muted rounded-xl px-3 py-2.5">
                <b className="block font-mono text-lg font-semibold">{value}</b>
                <small className="text-muted-foreground text-[11.5px]">{label}</small>
              </div>
            ))}
          </div>
          {skipped.length > 0 && segments.length > 0 && (
            <div className="bg-primary/10 text-muted-foreground flex gap-2.5 rounded-xl px-3 py-2.5 text-[12.5px]">
              <TriangleAlert className="text-primary mt-0.5 size-4 shrink-0" />
              <span>
                {skippedText(skipped)} 没有采用的视频，合成时会被跳过。
                <button
                  type="button"
                  className="text-primary ml-1 underline-offset-2 hover:underline"
                  onClick={() => onGo("studio", no)}
                >
                  去补齐
                </button>
              </span>
            </div>
          )}
          <ul className="grid gap-0.5">
            {segments.map((segment) => {
              const take = playableTake(segment);
              return (
                <li
                  key={segment.id}
                  className="hover:bg-muted/60 flex items-center gap-2.5 rounded-[10px] p-2 text-[13px]"
                >
                  <span
                    style={{ aspectRatio: aspectOf(drama.ratio) }}
                    className="bg-muted relative w-[30px] shrink-0 overflow-hidden rounded-[5px]"
                  >
                    {take?.asset && <VideoPoster src={take.asset.url} />}
                  </span>
                  <span className="min-w-0 flex-1">
                    片段 {String(segment.seq + 1).padStart(2, "0")}
                    <small className="text-muted-foreground block text-[11.5px]">
                      {segment.seconds}s
                    </small>
                  </span>
                  {take ? (
                    <span className="bg-status-success/15 text-status-success inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium">
                      <Check className="size-3" aria-hidden />
                      已采用
                    </span>
                  ) : segment.status === "running" ? (
                    <span className="bg-primary/15 text-primary rounded-md px-1.5 py-0.5 text-[11px] font-medium">
                      生成中
                    </span>
                  ) : (
                    <span className="bg-muted text-muted-foreground rounded-md px-1.5 py-0.5 text-[11px]">
                      跳过
                    </span>
                  )}
                </li>
              );
            })}
            {segments.length === 0 && (
              <li className="text-muted-foreground px-2 py-6 text-center text-xs">
                这一集还没有片段
              </li>
            )}
          </ul>
        </div>
        <div className="border-border border-t p-3.5">
          {running ? (
            <div>
              <div className="mb-1.5 flex justify-between text-[12.5px]">
                <span className="text-primary font-medium">正在合成…</span>
                <span className="font-mono">{running.progress}%</span>
              </div>
              <div className="bg-muted h-1 overflow-hidden rounded-full">
                <div
                  className="bg-primary h-full rounded-full transition-[width] duration-300"
                  style={{ width: `${running.progress}%` }}
                />
              </div>
            </div>
          ) : (
            <Button
              className="w-full"
              variant={state === "fresh" ? "outline" : "default"}
              disabled={!check.enabled || composing}
              title={check.reason ?? "按顺序拼接各片段采用的视频，不扣积分"}
              onClick={() => void compose(no)}
            >
              {composing ? <Loader2 className="animate-spin" /> : <Combine />}
              {state === "none" ? `合成第 ${no} 集` : "重新合成"}
            </Button>
          )}
          {check.reason && !running && (
            <p className="text-muted-foreground mt-2 text-center text-xs">{check.reason}</p>
          )}
        </div>
      </aside>
    </div>
  );
}
