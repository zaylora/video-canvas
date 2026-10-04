import { useEffect, useId, useRef, useState } from "react";
import { Play } from "lucide-react";
import { motion } from "motion/react";

import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { createVideoSlotPool, formatMediaDuration } from "@/utils/canvas/media-lod";

import { VideoPoster } from "./media-preview";

/** 全页面共用的 video 名额池：同时挂载的 video 受 WebMediaPlayer 数量限制 */
const videoSlots = createVideoSlotPool();

/** 原生控制条的大致高度；它在 shadow DOM 里，没法单独挂 nodrag */
const CONTROLS_HEIGHT = 64;

/**
 * video 铺满节点，整块都 nodrag 会导致节点拖不动。
 * 这里只在指针落在底部控制条时才加 nodrag（拖进度条不带动节点），画面区域仍可拖节点。
 * React Flow 在 mousedown 时才读 class，而 pointermove/pointerdown 都先于它触发。
 */
function syncDragGuard(e: React.PointerEvent<HTMLVideoElement>) {
  const el = e.currentTarget;
  const { bottom } = el.getBoundingClientRect();
  el.classList.toggle("nodrag", e.clientY >= bottom - CONTROLS_HEIGHT);
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
      // nowheel 把滚轮留给画布；nodrag 由 syncDragGuard 按指针位置动态加减
      <video
        ref={videoRef}
        src={src}
        controls
        playsInline
        autoPlay
        preload="auto"
        onPointerMove={syncDragGuard}
        onPointerDown={syncDragGuard}
        className="nowheel size-full object-contain"
      />
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
