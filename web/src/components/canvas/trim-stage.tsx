import { useCallback, useEffect, useRef, useState } from "react";
import { Pause, Play, Volume2, VolumeX } from "lucide-react";

import { useTrimStore } from "@/store/trim";
import { formatMediaDuration } from "@/utils/canvas/media-lod";

import { BaseNodeContent } from "./base-node";

/** 秒 → m:ss */
const clock = (seconds: number) => formatMediaDuration(seconds * 1000) ?? "0:00";

/**
 * 修剪模式下节点正文里的预览播放器：取代平时的 VideoPlayer。
 * 独立的 video，不占全页面的播放名额池（修剪中不能被别的视频开播时收走）。
 * 只在选区内播放：播到出点就暂停并回到入点；拖手柄时由仓库的 seek 请求把画面跳到被拖的那一端。
 * 进度条覆盖整段视频，选区外压暗、选区内高亮。
 */
export function TrimStage({
  src,
  aspect,
  poster,
}: {
  src: string;
  aspect: number;
  poster?: string;
}) {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const [paused, setPaused] = useState(true);
  const [failed, setFailed] = useState(false);
  const start = useTrimStore((state) => state.start);
  const end = useTrimStore((state) => state.end);
  const duration = useTrimStore((state) => state.duration);
  const current = useTrimStore((state) => state.current);
  const seek = useTrimStore((state) => state.seek);
  // 预览自己的静音：只管现在听不听，和下面「去音轨」（决定导出有没有声音）互不联动
  const [muted, setMuted] = useState(false);
  const locked = useTrimStore((state) => state.phase === "encoding");

  // 选区边界放进 ref，播放过程中的事件回调读最新值，不用因为拖手柄反复重挂监听
  const range = useRef({ start, end });
  useEffect(() => {
    range.current = { start, end };
  }, [start, end]);

  // src 手动设置，和 VideoPlayer 一样：严格模式下清理会摘掉 src，JSX 属性写法补不回来
  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    el.src = src;
    return () => {
      el.pause();
      el.removeAttribute("src");
      el.load();
    };
  }, [src]);

  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    const sync = () => {
      const { start: from, end: to } = range.current;
      // 播到出点：停住并回到入点；用小余量吃掉 timeupdate 的粒度（约 250ms 一次）
      if (!el.paused && el.currentTime >= to - 0.02) {
        el.pause();
        el.currentTime = from;
      }
      useTrimStore.getState().setCurrent(Math.round(el.currentTime * 100) / 100);
      setPaused(el.paused);
    };
    const events = ["play", "pause", "ended", "timeupdate", "seeked"];
    events.forEach((name) => el.addEventListener(name, sync));
    return () => events.forEach((name) => el.removeEventListener(name, sync));
  }, []);

  // 手柄拖动 / 点胶片条：把画面跳到那个时刻
  useEffect(() => {
    const el = videoRef.current;
    if (!el || !seek) return;
    el.currentTime = seek.time;
  }, [seek]);

  useEffect(() => {
    const el = videoRef.current;
    if (el) el.muted = muted;
  }, [muted]);

  const toggle = useCallback(() => {
    const el = videoRef.current;
    if (!el || locked) return;
    if (!el.paused) {
      el.pause();
      return;
    }
    const { start: from, end: to } = range.current;
    // 在选区外或已经在出点：从入点开始
    if (el.currentTime < from || el.currentTime >= to - 0.05) el.currentTime = from;
    void el.play();
  }, [locked]);

  const total = duration || 1;
  const left = (start / total) * 100;
  const width = ((end - start) / total) * 100;

  return (
    <BaseNodeContent>
      <div
        className="relative w-full overflow-hidden rounded-[inherit] bg-black"
        style={{ aspectRatio: aspect }}
      >
        <video
          ref={videoRef}
          crossOrigin="anonymous"
          poster={poster}
          muted={muted}
          preload="auto"
          playsInline
          disablePictureInPicture
          onError={() => setFailed(true)}
          onClick={toggle}
          className="size-full object-contain"
        />
        {failed && (
          <div className="absolute inset-0 grid place-items-center bg-black/70 text-sm text-white/80">
            读不出视频画面
          </div>
        )}
        <div className="nodrag nowheel absolute inset-x-0 bottom-0 flex items-center gap-3 bg-gradient-to-t from-black/50 to-transparent px-4 pt-10 pb-3 text-white">
          <button
            type="button"
            aria-label={paused ? "播放" : "暂停"}
            disabled={locked}
            onClick={toggle}
            className="grid size-6 shrink-0 place-items-center disabled:opacity-40"
          >
            {paused ? (
              <Play className="size-[18px] fill-current" />
            ) : (
              <Pause className="size-[18px] fill-current" />
            )}
          </button>
          <span className="shrink-0 font-mono text-[12px] tabular-nums text-white/90">
            {clock(current)}
          </span>
          <div className="relative h-1 min-w-0 flex-1 rounded-full bg-white/25">
            <div
              className="absolute inset-y-0 rounded-full bg-white/60"
              style={{ left: `${left}%`, width: `${width}%` }}
            />
            <div
              className="absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-white"
              style={{ left: `${(current / total) * 100}%` }}
            />
          </div>
          <span className="shrink-0 font-mono text-[12px] tabular-nums text-white/90">
            {clock(duration)}
          </span>
          <button
            type="button"
            aria-label={muted ? "预览开声音" : "预览静音"}
            aria-pressed={muted}
            onClick={() => setMuted((value) => !value)}
            className="grid size-6 shrink-0 place-items-center"
          >
            {muted ? <VolumeX className="size-[18px]" /> : <Volume2 className="size-[18px]" />}
          </button>
        </div>
      </div>
    </BaseNodeContent>
  );
}
