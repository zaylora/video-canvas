import { Pause, Play } from "lucide-react";
import { motion } from "motion/react";

import type { ShowcasePlayer } from "@/hooks/use-showcase-player";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 背景轮播的进度：每条作品一小段，当前段随播放从空走到满，点哪段就跳到哪条；
 * 旁边是「1 / 6」和暂停 / 播放按钮。自动轮换超过 5 秒必须能暂停（WCAG 2.2.2），
 * 所以只要有东西在动（视频在播或会自动切换），暂停按钮就一直在，只有一条时也在。
 * 进度段的 CSS 动画一走完就通知播放器切下一条，计时和画面因此始终一致。
 * @param tone stage 是压在视频上的玻璃样式；plain 是后台预览里的普通样式
 * @param showPause 是否显示暂停 / 播放按钮；登录页按设计去掉了它，后台预览保留
 */
export function ShowcaseProgress({
  player,
  tone = "stage",
  showPause = true,
  className,
}: {
  player: ShowcasePlayer;
  tone?: "stage" | "plain";
  showPause?: boolean;
  className?: string;
}) {
  const { count, index, running, autoAdvance, paused, playVideo, clipMs, advance, goTo } = player;
  const showControls = showPause && (playVideo || autoAdvance);
  if (count === 0 || (count === 1 && !showControls)) return null;
  const onStage = tone === "stage";

  return (
    <div
      data-slot="showcase-progress"
      className={cn(
        "flex items-center gap-2.5",
        onStage ? "text-on-stage" : "text-foreground",
        className,
      )}
    >
      {count > 1 && (
        <>
          <div className="flex gap-1">
            {Array.from({ length: count }, (_, i) => (
              <button
                key={i}
                type="button"
                aria-label={`第 ${i + 1} 条，共 ${count} 条`}
                aria-current={i === index}
                onClick={() => goTo(i)}
                className="group/seg flex h-5 w-6.5 items-center outline-none"
              >
                <span
                  className={cn(
                    "relative block h-0.5 w-full overflow-hidden rounded-full transition-[height] duration-120 group-hover/seg:h-1 group-focus-visible/seg:h-1",
                    onStage ? "bg-stage-glass-border" : "bg-foreground/20",
                  )}
                >
                  {/* 正在走的这一段不能带 scale-x-*：Tailwind v4 用独立的 scale 属性，会和动画里的 transform 相乘成 0 */}
                  <b
                    key={`${i}-${index}`}
                    className={cn(
                      "absolute inset-0 origin-left rounded-full",
                      onStage ? "bg-on-stage" : "bg-foreground",
                      i < index && "scale-x-100",
                      i > index && "scale-x-0",
                      i === index && !autoAdvance && "scale-x-100",
                    )}
                    style={
                      i === index && autoAdvance
                        ? {
                            animation: `showcase-fill ${clipMs}ms linear forwards`,
                            animationPlayState: running ? "running" : "paused",
                          }
                        : undefined
                    }
                    onAnimationEnd={i === index ? advance : undefined}
                  />
                </span>
              </button>
            ))}
          </div>
          <span
            className={cn(
              "min-w-9 text-right text-xs tabular-nums",
              onStage ? "text-on-stage-muted" : "text-muted-foreground",
            )}
          >
            {index + 1} / {count}
          </span>
        </>
      )}
      {showControls && (
        <motion.button
          type="button"
          whileTap={TAP}
          onClick={player.togglePause}
          aria-label={paused ? "播放背景视频" : "暂停背景视频"}
          title={paused ? "播放背景视频" : "暂停背景视频"}
          className={cn(
            "focus-visible:ring-ring/60 grid size-8 place-items-center rounded-full border outline-none focus-visible:ring-2",
            onStage
              ? "bg-stage-glass border-stage-glass-border hover:bg-stage-glass-border backdrop-blur-md"
              : "bg-muted border-border hover:bg-foreground/10",
          )}
        >
          {paused ? <Play className="size-3.5 fill-current" /> : <Pause className="size-3.5" />}
        </motion.button>
      )}
    </div>
  );
}
