import { CircleX, Film, Loader2, Play } from "lucide-react";

import type { Script, TaskLite } from "@/api/marketing/type";
import { cn } from "@/lib/utils";
import { roundAspect } from "@/utils/marketing/script";

/** 脚本卡左侧的成片区：待出片、重写中、出片中（进度环）、已出片（可播放、多条切换）、失败（写原因） */
export function VideoBox({
  script,
  params,
  posterUrl,
  progress,
  takes,
  current,
  onSelectTake,
  onPlay,
}: {
  script: Script;
  /** 轮次设置里的视频参数，取画幅 */
  params: Record<string, unknown>;
  /** 没有成片首帧时的底图：商品图 */
  posterUrl: string;
  /** 出片进度 0–100；还没有为 null */
  progress: number | null;
  /** 有成片的 take，按时间正序 */
  takes: TaskLite[];
  /** 当前显示第几条 */
  current: number;
  onSelectTake: (index: number) => void;
  onPlay: () => void;
}) {
  const [w, h] = roundAspect({ params });
  const done = script.status === "done" && takes.length > 0;
  const take = takes[Math.min(current, takes.length - 1)];
  const poster = (done && take?.outputs[0]?.posterUrl) || posterUrl;
  const pct = Math.min(100, Math.max(0, Math.round(progress ?? 0)));
  const circumference = 2 * Math.PI * 20;

  return (
    <div
      data-slot="marketing-video-box"
      style={{ aspectRatio: `${w} / ${h}` }}
      className={cn(
        "bg-muted ring-chrome-border relative w-full shrink-0 overflow-hidden rounded-2xl ring-1",
        w > h ? "sm:w-72" : "max-h-72 sm:w-auto sm:max-w-44",
      )}
    >
      {poster && (
        <img
          src={poster}
          alt=""
          className={cn(
            "absolute inset-0 size-full object-cover",
            !done && "scale-125 blur-md saturate-50",
          )}
        />
      )}
      {script.status === "rendering" && (
        <div className="bg-cover-scrim/40 text-cover-foreground absolute inset-0 grid place-content-center justify-items-center gap-1.5 text-xs">
          <svg viewBox="0 0 48 48" className="size-12 -rotate-90" aria-hidden>
            <circle
              cx="24"
              cy="24"
              r="20"
              fill="none"
              stroke="currentColor"
              strokeOpacity=".25"
              strokeWidth="3"
            />
            <circle
              cx="24"
              cy="24"
              r="20"
              fill="none"
              stroke="currentColor"
              strokeWidth="3"
              strokeLinecap="round"
              strokeDasharray={circumference}
              strokeDashoffset={circumference * (1 - pct / 100)}
              className="transition-[stroke-dashoffset] duration-300"
            />
          </svg>
          <span className="tabular-nums">出片中 {pct}%</span>
        </div>
      )}
      {script.status === "rewriting" && (
        <div className="bg-cover-scrim/40 text-cover-foreground absolute inset-0 grid place-content-center justify-items-center gap-1.5 text-xs">
          <Loader2 className="size-5 animate-spin" />
          重写中
        </div>
      )}
      {script.status === "failed" && (
        <div className="bg-cover-scrim/55 text-cover-foreground absolute inset-0 grid place-content-center justify-items-center gap-1.5 px-3 text-center text-xs">
          <CircleX className="size-5" />
          <span className="line-clamp-4">{script.error || "出片失败"}</span>
        </div>
      )}
      {script.status === "draft" && (
        <div className="bg-cover-scrim/25 text-cover-foreground absolute inset-0 grid place-content-center justify-items-center gap-1.5 text-xs">
          <Film className="size-5" />
          还没出片
        </div>
      )}
      {done && (
        <>
          <button
            type="button"
            onClick={onPlay}
            aria-label="播放成片"
            className="focus-visible:ring-ring/60 absolute inset-0 grid place-items-center outline-none focus-visible:ring-3"
          >
            <span className="bg-cover-scrim text-cover-foreground grid size-11 place-items-center rounded-full">
              <Play className="size-4.5 fill-current" />
            </span>
          </button>
          {takes.length > 1 && (
            <div className="absolute inset-x-0 bottom-1.5 flex justify-center gap-1">
              {takes.map((item, index) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => onSelectTake(index)}
                  title={`第 ${index + 1} 条`}
                  aria-pressed={index === current}
                  className={cn(
                    "bg-cover-scrim text-cover-foreground focus-visible:ring-ring/60 size-5 rounded-full text-[11px] tabular-nums outline-none focus-visible:ring-2",
                    index === current && "bg-cover-foreground text-foreground",
                  )}
                >
                  {index + 1}
                </button>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
