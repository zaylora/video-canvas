import { useEffect, useRef, useState } from "react";
import { Volume2, VolumeX } from "lucide-react";

import { useFramePickStore } from "@/store/frame-pick";

import { BaseNodeContent } from "./base-node";

/**
 * 选帧模式下节点正文里的预览播放器：取代平时的 VideoPlayer。
 * 独立的 video，不占全页面的播放名额池；播放和定位都听 useFramePickStore 的：
 * 面板的播放键写 playing，点胶片条、方向键写 seek，播放时把进度写回 current 给播放头用。
 * 只负责「看」：截取走 Mediabunny 精确取帧，不从这里的 video 截。
 */
export function FrameStage({
  src,
  aspect,
  poster,
}: {
  src: string;
  aspect: number;
  poster?: string;
}) {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const [failed, setFailed] = useState(false);
  // 预览自己的静音：只管现在听不听
  const [muted, setMuted] = useState(false);
  const playing = useFramePickStore((state) => state.playing);
  const seek = useFramePickStore((state) => state.seek);

  // src 手动设置，和 TrimStage 一样：严格模式下清理会摘掉 src，JSX 属性写法补不回来
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

  // video 自己的播放、暂停、播完，写回仓库，播放键的图标才对得上
  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    const sync = () => useFramePickStore.getState().setPlaying(!el.paused && !el.ended);
    const events = ["play", "pause", "ended"];
    events.forEach((name) => el.addEventListener(name, sync));
    return () => events.forEach((name) => el.removeEventListener(name, sync));
  }, []);

  // 播放中每帧把进度写回；停下后再补一次，拖定位后的 seeked 也要更新
  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    const write = () =>
      useFramePickStore.getState().setCurrent(Math.round(el.currentTime * 100) / 100);
    el.addEventListener("seeked", write);
    el.addEventListener("pause", write);
    if (!playing)
      return () => {
        el.removeEventListener("seeked", write);
        el.removeEventListener("pause", write);
      };
    let raf = 0;
    const tick = () => {
      write();
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(raf);
      el.removeEventListener("seeked", write);
      el.removeEventListener("pause", write);
    };
  }, [playing]);

  // 面板的播放键
  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    if (playing && el.paused) {
      if (el.ended || el.currentTime >= el.duration - 0.05) el.currentTime = 0;
      void el.play().catch(() => useFramePickStore.getState().setPlaying(false));
    } else if (!playing && !el.paused) {
      el.pause();
    }
  }, [playing]);

  // 点胶片条、方向键：把画面跳到那个时刻
  useEffect(() => {
    const el = videoRef.current;
    if (!el || !seek) return;
    el.currentTime = seek.time;
  }, [seek]);

  useEffect(() => {
    const el = videoRef.current;
    if (el) el.muted = muted;
  }, [muted]);

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
          className="size-full object-contain"
        />
        {failed && (
          <div className="absolute inset-0 grid place-items-center bg-black/70 text-sm text-white/80">
            读不出视频画面
          </div>
        )}
        <button
          type="button"
          aria-label={muted ? "预览开声音" : "预览静音"}
          aria-pressed={muted}
          onClick={() => setMuted((value) => !value)}
          className="nodrag absolute right-3 bottom-3 grid size-7 place-items-center rounded-full bg-black/55 text-white"
        >
          {muted ? <VolumeX className="size-4" /> : <Volume2 className="size-4" />}
        </button>
      </div>
    </BaseNodeContent>
  );
}
