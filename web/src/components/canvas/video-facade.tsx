import { useEffect, useId, useRef, useState, type RefObject } from "react";
import { Pause, Play, Volume2, VolumeX } from "lucide-react";
import { motion } from "motion/react";

import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { createVideoSlotPool, formatMediaDuration } from "@/utils/canvas/media-lod";

import { VideoPoster } from "./media-preview";

/** 全页面共用的 video 名额池：同时挂载的 video 受 WebMediaPlayer 数量限制 */
const videoSlots = createVideoSlotPool();

/** 秒 → m:ss；还没有有效时长时显示 0:00 */
function formatClock(seconds: number): string {
  return formatMediaDuration(seconds * 1000) ?? "0:00";
}

type VideoControlsProps = {
  videoRef: RefObject<HTMLVideoElement | null>;
};

/**
 * 自绘的精简控制条：播放/暂停、进度条、时间、静音，单行排布。
 * 原生控制条的布局和全屏、更多菜单都没法裁掉，所以不用 `controls`。
 * 整条 nodrag nowheel，拖进度条不会带动节点，视频画面区域仍可拖节点。
 */
function VideoControls({ videoRef }: VideoControlsProps) {
  const [paused, setPaused] = useState(false);
  const [muted, setMuted] = useState(false);
  const [current, setCurrent] = useState(0);
  const [total, setTotal] = useState(0);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const sync = () => {
      setPaused(video.paused);
      setMuted(video.muted);
      setCurrent(video.currentTime);
      setTotal(Number.isFinite(video.duration) ? video.duration : 0);
    };
    sync();
    const events = [
      "play",
      "pause",
      "ended",
      "timeupdate",
      "durationchange",
      "loadedmetadata",
      "volumechange",
    ];
    events.forEach((name) => video.addEventListener(name, sync));
    return () => events.forEach((name) => video.removeEventListener(name, sync));
  }, [videoRef]);

  const togglePlay = () => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused || video.ended) void video.play();
    else video.pause();
  };

  const percent = total > 0 ? (current / total) * 100 : 0;

  return (
    <div className="nodrag nowheel absolute inset-x-0 bottom-0 flex items-center gap-3.5 bg-gradient-to-t from-black/45 to-transparent px-4 pt-10 pb-3.5 text-white">
      <button
        type="button"
        aria-label={paused ? "播放" : "暂停"}
        onClick={togglePlay}
        className="grid size-6 shrink-0 place-items-center"
      >
        {paused ? (
          <Play className="size-[18px] fill-current" />
        ) : (
          <Pause className="size-[18px] fill-current" />
        )}
      </button>
      <input
        type="range"
        aria-label="播放进度"
        min={0}
        max={total || 1}
        step="any"
        value={current}
        onChange={(e) => {
          const video = videoRef.current;
          if (video) video.currentTime = Number(e.target.value);
        }}
        style={{
          background: `linear-gradient(to right, #fff ${percent}%, rgb(255 255 255 / 0.35) ${percent}%)`,
        }}
        className="h-0.5 min-w-0 flex-1 cursor-pointer appearance-none rounded-full [&::-moz-range-thumb]:size-2.5 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:bg-white [&::-webkit-slider-thumb]:size-2.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-white"
      />
      <span className="shrink-0 text-[13px] tabular-nums text-white/90">
        {formatClock(current)} / {formatClock(total)}
      </span>
      <button
        type="button"
        aria-label={muted ? "取消静音" : "静音"}
        onClick={() => {
          const video = videoRef.current;
          if (video) video.muted = !video.muted;
        }}
        className="grid size-6 shrink-0 place-items-center"
      >
        {muted ? <VolumeX className="size-[18px]" /> : <Volume2 className="size-[18px]" />}
      </button>
    </div>
  );
}

type VideoFacadeProps = {
  /** 视频地址（不带变体） */
  src: string;
  /** 视频时长，毫秒；节点数据里没有就不传，不显示角标 */
  durationMs?: number | null;
};

/**
 * 视频节点的 facade：平时只显示封面、播放按钮和时长角标，不挂载 video，
 * 首屏零视频文件请求；点击后才挂载 video 并自动播放。
 * 同时挂载的 video 超过名额池上限时，已暂停的会被退回封面，播放中的不打断。
 * 填满父级盒子，画幅由父级决定。
 */
export function VideoFacade({ src, durationMs }: VideoFacadeProps) {
  const id = useId();
  const [playing, setPlaying] = useState(false);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const duration = formatMediaDuration(durationMs);

  useEffect(() => {
    const el = videoRef.current;
    if (!playing || !el) return;
    videoSlots.acquire(id, {
      isPlaying: () => !el.paused && !el.ended,
      release: () => setPlaying(false),
    });
    return () => {
      videoSlots.release(id);
      // 只摘掉 DOM 不会释放 WebMediaPlayer，必须清掉 src 并 load()
      el.pause();
      el.removeAttribute("src");
      el.load();
    };
  }, [playing, id]);

  if (playing) {
    return (
      <div className="relative size-full">
        <video
          ref={videoRef}
          src={src}
          playsInline
          autoPlay
          preload="auto"
          className="size-full object-contain"
        />
        <VideoControls videoRef={videoRef} />
      </div>
    );
  }

  return (
    <div className="relative size-full">
      <VideoPoster src={src} className="object-contain" />
      <motion.button
        type="button"
        aria-label="播放视频"
        whileHover={{ scale: 1.06 }}
        whileTap={TAP}
        transition={{ duration: DURATION.fast, ease: EASE_OUT }}
        onClick={() => setPlaying(true)}
        className="nodrag absolute inset-0 m-auto grid size-12 place-items-center rounded-full bg-black/55 text-white shadow-lg backdrop-blur"
      >
        <Play className="size-5 translate-x-px fill-current" />
      </motion.button>
      {duration && (
        <span className="pointer-events-none absolute right-2 bottom-2 rounded-md bg-black/60 px-1.5 py-0.5 text-[11px] leading-4 text-white tabular-nums">
          {duration}
        </span>
      )}
    </div>
  );
}
