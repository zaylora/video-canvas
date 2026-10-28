import { CircleX, Clapperboard, Loader2 } from "lucide-react";

import { VideoPoster } from "@/components/canvas/media-preview";
import { SHIMMER } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { playableTake } from "@/utils/drama/segments";

import { useStudio } from "./use-studio";

/** 「x:y」比例转成 CSS aspect-ratio；认不出来用 9:16 */
const aspectOf = (ratio: string) => (/^\d+:\d+$/.test(ratio) ? ratio.replace(":", "/") : "9/16");

/** 右预览：采用的视频播放；多次结果的缩略图，点选即采用 */
export function Preview() {
  const { selected, drama, actions, submitting } = useStudio();
  const take = selected ? playableTake(selected) : null;
  const running = !!selected && (selected.status === "running" || submitting.has(selected.id));
  return (
    <aside
      aria-label="预览"
      className="border-border flex w-[300px] shrink-0 flex-col gap-3 border-l p-4 max-lg:hidden"
    >
      <b className="text-sm">预览</b>
      <div
        style={{ aspectRatio: aspectOf(drama.ratio) }}
        className="bg-muted relative mx-auto grid max-h-[420px] w-full place-items-center overflow-hidden rounded-[14px]"
      >
        {take?.asset ? (
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
