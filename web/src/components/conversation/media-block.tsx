import { Music, Play } from "lucide-react";

import { cn } from "@/lib/utils";
import { placeholderBackground } from "@/utils/home/placeholder";
import type { ConversationResult } from "@/types";

/** 各画幅的尺寸：高度固定，宽度由宽高比决定；small 是分镜图这类一行放好几张的缩略尺寸 */
const SHAPE = {
  square: "h-48 aspect-square md:h-60",
  wide: "h-48 aspect-video md:h-60",
  tall: "h-60 aspect-[9/16] md:h-75",
} as const;

/**
 * 对话记录里的一个生成结果。生成中显示脉动的占位和进度；
 * 完成后是封面（视频叠播放键、分镜叠序号），音频是一条波形。
 * 现在封面是占位渐变，接口就绪后换成素材地址，点开预览也在那时接。
 * @param result 结果
 * @param progress 所在记录的进度，null 表示已完成
 */
export function MediaBlock({
  result,
  progress,
}: {
  result: ConversationResult;
  progress: number | null;
}) {
  const pending = progress !== null;

  if (result.kind === "audio") {
    return (
      <div
        data-slot="media-block"
        className={cn(
          "bg-muted flex h-18 w-full max-w-90 items-center gap-3 rounded-xl px-3.5",
          pending && "animate-pulse",
        )}
      >
        <Music className="text-muted-foreground size-4 shrink-0" />
        <span className="h-7 flex-1 [mask-image:linear-gradient(transparent,#000_30%,#000_70%,transparent)] bg-[repeating-linear-gradient(90deg,color-mix(in_oklch,var(--foreground)_45%,transparent)_0_2px,transparent_2px_5px)]" />
        <span className="text-muted-foreground text-xs tabular-nums">
          {pending ? `${progress}%` : (result.duration ?? "0:30")}
        </span>
      </div>
    );
  }

  const size = result.small ? "h-28 aspect-video md:h-33" : SHAPE[result.shape];

  if (pending) {
    return (
      <div
        data-slot="media-block"
        role="status"
        aria-label={`生成中 ${progress}%`}
        className={cn(
          "bg-muted text-muted-foreground grid animate-pulse place-items-center rounded-xl text-[12.5px] tabular-nums",
          size,
        )}
      >
        生成中 · {progress}%
      </div>
    );
  }

  return (
    <button
      type="button"
      data-slot="media-block"
      aria-label={result.kind === "video" ? "播放视频" : "查看大图"}
      style={{ background: placeholderBackground(result.hue) }}
      className={cn(
        "focus-visible:ring-ring/60 relative overflow-hidden rounded-xl outline-none focus-visible:ring-3",
        size,
      )}
    >
      {result.shot && (
        <span className="bg-cover-scrim text-cover-foreground absolute top-2 left-2 inline-grid h-5 place-items-center rounded-md px-1.5 text-[11px] tabular-nums">
          分镜 {result.shot}
        </span>
      )}
      {result.kind === "video" && (
        <>
          <span className="absolute inset-0 m-auto grid size-11 place-items-center rounded-full bg-cover-scrim text-cover-foreground backdrop-blur-sm">
            <Play className="size-4 fill-current" />
          </span>
          {result.duration && (
            <span className="bg-cover-scrim text-cover-foreground absolute right-2 bottom-2 inline-grid h-5 place-items-center rounded-md px-1.5 text-[11px] tabular-nums">
              {result.duration}
            </span>
          )}
        </>
      )}
    </button>
  );
}
