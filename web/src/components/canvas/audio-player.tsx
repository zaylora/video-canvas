import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import { Pause, Play } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";

import { buttonVariants } from "@/components/ui/button";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  SILENT_PEAK,
  clockParts,
  positionToTime,
  timeToPosition,
} from "@/utils/canvas/audio-peaks";

import { useAudioPeaks } from "./hooks/use-audio-peaks";

/** 整首音频均分成几根竖条 */
const BARS = 56;

/** 键盘左右键一次挪多少秒 */
const KEY_STEP = 1;

type AudioPlayerProps = {
  /** 音频地址 */
  src: string;
  /** 所在节点是否被选中；失去选中会暂停，默认不受约束 */
  active?: boolean;
};

/**
 * 音频节点的正文（设计 6.18）：整首波形静止铺满，播放头沿波形走，拖播放头（或在波形上按下）调进度，
 * 拖动时暂停、松手后若拖之前在播就接着播。填满父级盒子，画幅由父级（1:1）决定。
 * 播放由隐藏的 audio 管；播放头只改 transform，竖条只在播放头跨过它时才重渲染。
 */
export function AudioPlayer({ src, active = true }: AudioPlayerProps) {
  const reduce = useReducedMotion();
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const headRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<{ wasPlaying: boolean } | null>(null);
  const { peaks, duration: decoded } = useAudioPeaks(src, BARS);

  const [playing, setPlaying] = useState(false);
  const [failed, setFailed] = useState(false);
  const [metaDuration, setMetaDuration] = useState(0);
  /** 当前时间的整秒数，时间文字一秒才变一次 */
  const [second, setSecond] = useState(0);
  /** 播放头已跨过几根竖条 */
  const [playedBars, setPlayedBars] = useState(0);
  const duration = metaDuration || decoded;

  /** 按当前时间摆好播放头、更新已播竖条和时间；不触发布局 */
  const paint = useCallback(
    (time: number) => {
      const position = timeToPosition(time, duration);
      if (headRef.current) headRef.current.style.transform = `translateX(${position * 100}cqw)`;
      setPlayedBars(Math.floor(position * BARS));
      setSecond(Math.floor(time));
    },
    [duration],
  );

  // 播放中每帧读 currentTime 驱动播放头
  useEffect(() => {
    if (!playing) return;
    let frame = 0;
    const tick = () => {
      const audio = audioRef.current;
      if (audio) paint(audio.currentTime);
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [playing, paint]);

  // 时长出来、换素材后，把播放头摆回当前位置
  useEffect(() => {
    paint(audioRef.current?.currentTime ?? 0);
  }, [paint]);

  // 节点失去选中：暂停
  useEffect(() => {
    if (!active) audioRef.current?.pause();
  }, [active]);

  const toggle = () => {
    const audio = audioRef.current;
    if (!audio || failed) return;
    if (audio.paused || audio.ended) void audio.play().catch(() => setFailed(true));
    else audio.pause();
  };

  const seekTo = (time: number) => {
    const audio = audioRef.current;
    if (!audio || !(duration > 0)) return;
    audio.currentTime = Math.min(duration, Math.max(0, time));
    paint(audio.currentTime);
  };

  /** 指针位置换成时间：用元素自己的屏幕宽度算比例，画布缩放时也准 */
  const seekFromPointer = (event: PointerEvent<HTMLDivElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    seekTo(positionToTime((event.clientX - rect.left) / rect.width, duration));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const delta = event.key === "ArrowRight" ? KEY_STEP : event.key === "ArrowLeft" ? -KEY_STEP : 0;
    if (!delta) return;
    event.preventDefault();
    seekTo((audioRef.current?.currentTime ?? 0) + delta);
  };

  const clock = clockParts(second);

  return (
    <div className="flex size-full flex-col gap-4 p-5">
      <audio
        ref={audioRef}
        src={src}
        preload="metadata"
        onLoadedMetadata={(event) => {
          const value = event.currentTarget.duration;
          setMetaDuration(Number.isFinite(value) ? value : 0);
        }}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => {
          setPlaying(false);
          seekTo(0);
        }}
        onError={() => setFailed(true)}
      />
      {/* nodrag nowheel nopan：拖播放头不能把节点或画布带走 */}
      <div
        data-slot="audio-wave"
        className="nodrag nowheel nopan @container relative min-h-0 flex-1 cursor-ew-resize touch-none"
        onPointerDown={(event) => {
          if (failed) return;
          event.currentTarget.setPointerCapture(event.pointerId);
          dragRef.current = { wasPlaying: playing };
          audioRef.current?.pause();
          seekFromPointer(event);
        }}
        onPointerMove={(event) => {
          if (dragRef.current) seekFromPointer(event);
        }}
        onPointerUp={() => {
          const drag = dragRef.current;
          dragRef.current = null;
          if (drag?.wasPlaying) void audioRef.current?.play().catch(() => setFailed(true));
        }}
        onPointerCancel={() => {
          dragRef.current = null;
        }}
      >
        <motion.div
          key={peaks ? "ready" : "empty"}
          className="absolute inset-0 flex items-center justify-between"
          initial={{ opacity: peaks && !reduce ? 0 : 1 }}
          animate={{ opacity: 1 }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
        >
          {Array.from({ length: BARS }, (_, i) => {
            const peak = peaks?.[i] ?? 0;
            return (
              <span
                key={i}
                aria-hidden
                className={cn(
                  "w-0.5 rounded-full",
                  i < playedBars ? "bg-foreground" : "bg-muted-foreground/60",
                  peaks === undefined && "motion-safe:animate-pulse",
                )}
                style={{ height: peak < SILENT_PEAK ? 2 : `${Math.max(peak, 0.08) * 100}%` }}
              />
            );
          })}
        </motion.div>
        {/* 播放头：热区比线宽，竖线和顶端圆点是 --status-running 色 */}
        <div
          ref={headRef}
          role="slider"
          tabIndex={0}
          aria-label="播放进度"
          aria-valuemin={0}
          aria-valuemax={Math.round(duration)}
          aria-valuenow={second}
          onKeyDown={onKeyDown}
          className="absolute -inset-y-2 left-0 -ml-3 w-6 outline-none will-change-transform focus-visible:[&>span]:ring-2 focus-visible:[&>span]:ring-node-ring"
        >
          <span className="bg-status-running absolute top-2 bottom-0 left-1/2 w-0.5 -translate-x-1/2 rounded-full ring-offset-2" />
          <span className="bg-status-running absolute top-1.5 left-1/2 size-1.5 -translate-x-1/2 rounded-full" />
        </div>
      </div>
      <div className="flex items-center justify-between">
        <span className="text-xl tabular-nums" aria-live="off">
          {failed ? (
            <span className="text-muted-foreground">--:--:--</span>
          ) : (
            <>
              <span className="text-muted-foreground">{clock.dim}</span>
              {clock.bright}
            </>
          )}
        </span>
        <motion.button
          type="button"
          aria-label={playing ? "暂停" : "播放"}
          disabled={failed}
          whileTap={TAP}
          onClick={toggle}
          className={cn(
            buttonVariants({ variant: "secondary", size: "icon-sm" }),
            "nodrag rounded-full",
          )}
        >
          {playing ? (
            <Pause className="fill-current" />
          ) : (
            <Play className="translate-x-px fill-current" />
          )}
        </motion.button>
      </div>
    </div>
  );
}
