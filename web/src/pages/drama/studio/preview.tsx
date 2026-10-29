import { AnimatePresence, motion } from "motion/react";
import { Clapperboard, Loader2 } from "lucide-react";

import { VideoPoster } from "@/components/canvas/media-preview";
import { EASE_OUT, SHIMMER } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { playableTake } from "@/utils/drama/segments";

import { TakeCarousel } from "./take-carousel";
import { usePlaybackContext } from "./use-playback";
import { useStudio } from "./use-studio";

/** 「x:y」比例转成 CSS aspect-ratio；认不出来用 9:16 */
const aspectOf = (ratio: string) => (/^\d+:\d+$/.test(ratio) ? ratio.replace(":", "/") : "9/16");

/** 「x:y」比例转成宽 / 高的数值；认不出来按 9:16 */
const aspectValue = (ratio: string) => {
  const match = /^(\d+):(\d+)$/.exec(ratio);
  return match ? Number(match[1]) / Number(match[2]) : 9 / 16;
};

/**
 * 播放器背后的氛围光：把当前画面的海报放大、虚化、压低透明度铺在底下，
 * 换片段时淡入淡出；没有画面时只有一团琥珀色的暗光。
 */
function Ambient({ src }: { src: string | null }) {
  return (
    <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden">
      <div
        className="absolute inset-0"
        style={{
          background:
            "radial-gradient(60% 50% at 50% 45%, color-mix(in oklch, var(--primary) 14%, transparent), transparent 70%)",
        }}
      />
      <AnimatePresence>
        {src && (
          <motion.div
            key={src}
            initial={{ opacity: 0 }}
            animate={{ opacity: 0.32 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.8, ease: EASE_OUT }}
            className="absolute -inset-[15%] blur-[80px] saturate-150"
          >
            <VideoPoster src={src} />
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

/**
 * 中间的预览：这个片段生成过结果时是版本轮播（正中是采用的那一版，滑到哪版就采用哪版）；
 * 连播时换成双缓冲的大播放器显示当前片段。成片在「成片」页看，不在这里。
 * 连播用的两个 video 一直挂着（轮播时只是藏起来），否则开始连播时拿不到它们。
 */
export function Preview() {
  const { selected, drama, actions, submitting, segments } = useStudio();
  const playback = usePlaybackContext();
  // 连播时预览跟着当前片段（选中也会跟过去，这里直接按连播的当前片段取，不等选中更新）
  const current = segments.find((item) => item.id === playback.currentId) ?? selected;
  const take = selected ? playableTake(selected) : null;
  const shownTake = playback.active && current ? playableTake(current) : take;
  const running = !!selected && (selected.status === "running" || submitting.has(selected.id));
  const carousel = !playback.active && !!selected && selected.takes.length > 0;
  return (
    <section
      aria-label="预览"
      className="relative flex h-full w-full min-w-0 flex-col items-center gap-3 overflow-hidden px-5 py-4"
    >
      <Ambient src={shownTake?.asset?.url ?? null} />
      <div className="relative z-10 flex min-h-0 w-full flex-1 items-center justify-center">
        {carousel && selected && (
          <div className="absolute inset-0">
            <TakeCarousel
              key={selected.id}
              takes={selected.takes}
              chosenTaskId={selected.chosenTaskId}
              aspect={aspectValue(drama.ratio)}
              onChoose={(taskId) => void actions.choose(selected, taskId)}
            />
          </div>
        )}
        <div
          aria-hidden={carousel}
          style={{ aspectRatio: aspectOf(drama.ratio) }}
          className={cn(
            "relative grid h-full max-h-[680px] max-w-full place-items-center overflow-hidden rounded-2xl bg-black shadow-[0_40px_80px_-30px_rgb(0_0_0/0.9)] ring-1 ring-white/10",
            carousel && "invisible",
          )}
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
            <div className="pointer-events-none absolute inset-x-0 bottom-0 flex justify-between bg-gradient-to-t from-black/70 to-transparent px-3 pt-6 pb-2 font-mono text-[11px] text-white">
              <span>
                片段 {String((current?.seq ?? 0) + 1).padStart(2, "0")}
                <span className="bg-primary/90 text-primary-foreground ml-2 rounded px-1.5 py-px font-sans text-[10px] font-medium">
                  连播{playback.playing ? "中" : "已暂停"}
                </span>
              </span>
              <span>{current?.seconds}s</span>
            </div>
          ) : running ? (
            <div className="grid size-full place-items-center bg-[repeating-linear-gradient(135deg,oklch(0.17_0_0)_0_14px,oklch(0.15_0_0)_14px_28px)] text-center text-xs text-white/70">
              <div>
                <Loader2 className="mx-auto mb-3 size-6 animate-spin" />
                <span
                  className="inline-block animate-pulse font-medium motion-reduce:animate-none"
                  style={{ animationDuration: `${SHIMMER}s` }}
                >
                  生成中，通常要几分钟
                </span>
              </div>
            </div>
          ) : (
            <div className="grid size-full place-items-center bg-[repeating-linear-gradient(135deg,oklch(0.17_0_0)_0_14px,oklch(0.15_0_0)_14px_28px)] text-center text-xs text-white/70">
              <div>
                <span className="mb-1 block font-mono text-4xl font-semibold tracking-tight text-white/20">
                  {String((selected?.seq ?? 0) + 1).padStart(2, "0")}
                </span>
                <Clapperboard className="mx-auto mb-1.5 size-5" />
                还没有生成内容{" "}
              </div>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
