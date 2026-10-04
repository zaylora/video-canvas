import { useEffect, useRef, useState } from "react";
import { Pause, Play, Repeat, Volume2, VolumeX } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DURATION } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatMediaDuration } from "@/utils/canvas/media-lod";

/** 倍速档位 */
const SPEEDS = [0.5, 1, 1.5, 2] as const;

/** 鼠标停多久没动就把控制条淡出，毫秒 */
const IDLE_MS = 2500;

type LightboxVideoProps = {
  /** 视频原文件地址 */
  src: string;
  /** 拿到视频自然尺寸后回调，外层据此算舞台画幅 */
  onSize?: (width: number, height: number) => void;
  /** 视频无法播放 */
  onError?: () => void;
  className?: string;
};

/** 秒 → m:ss；还没有有效时长时显示 0:00 */
function clock(seconds: number): string {
  return formatMediaDuration(seconds * 1000) ?? "0:00";
}

/**
 * 预览弹层里的视频：挂载即自动播放，卸载时释放媒体资源。
 * 控制条是自绘的单行：播放/暂停、进度、时间、倍速、循环、音量。
 * 鼠标 2.5 秒不动淡出，一动淡入；暂停时常显。
 * 空格切换播放、M 切换静音，由这里自己监听，弹层不用知道视频的存在。
 */
export function LightboxVideo({ src, onSize, onError, className }: LightboxVideoProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [paused, setPaused] = useState(false);
  const [muted, setMuted] = useState(false);
  const [loop, setLoop] = useState(false);
  const [speed, setSpeed] = useState<number>(1);
  const [current, setCurrent] = useState(0);
  const [total, setTotal] = useState(0);
  const [idle, setIdle] = useState(false);
  const idleTimer = useRef<number | undefined>(undefined);

  /** 动一下就亮起来，停一会再淡出 */
  const wake = () => {
    setIdle(false);
    window.clearTimeout(idleTimer.current);
    idleTimer.current = window.setTimeout(() => setIdle(true), IDLE_MS);
  };

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const sync = () => {
      setPaused(video.paused);
      setMuted(video.muted);
      setCurrent(video.currentTime);
      setTotal(Number.isFinite(video.duration) ? video.duration : 0);
    };
    /*
     * src 在这里手动设置而不是走 JSX 属性：下面的清理会摘掉 src 来释放媒体资源，
     * StrictMode 下副作用「执行→清理→再执行」，属性写法的 src 被摘掉后 React 不会补回来
     */
    video.src = src;
    void video.play().catch(() => {});
    const events = ["play", "pause", "ended", "timeupdate", "durationchange", "volumechange"];
    events.forEach((name) => video.addEventListener(name, sync));
    sync();
    wake();
    return () => {
      events.forEach((name) => video.removeEventListener(name, sync));
      window.clearTimeout(idleTimer.current);
      // 只摘掉 DOM 不会释放 WebMediaPlayer，必须清掉 src 并 load()
      video.pause();
      video.removeAttribute("src");
      video.load();
    };
  }, [src]);

  const togglePlay = () => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused || video.ended) void video.play();
    else video.pause();
  };
  const toggleMuted = () => {
    const video = videoRef.current;
    if (video) video.muted = !video.muted;
  };

  useEffect(() => {
    /*
     * 捕获阶段处理：焦点多半停在某个按钮或进度条上，冒泡阶段会被它们先吃掉。
     * 空格在这里统一当成「播放/暂停」，并拦下按钮自带的空格点击，免得焦点在下载键上时误触发下载
     */
    const onKey = (event: KeyboardEvent) => {
      if ((event.target as HTMLElement | null)?.closest("[role='menu']")) return;
      if (event.key === " ") {
        event.preventDefault();
        if (event.type === "keydown" && !event.repeat) {
          togglePlay();
          wake();
        }
      } else if (event.type === "keydown" && (event.key === "m" || event.key === "M")) {
        toggleMuted();
      }
    };
    document.addEventListener("keydown", onKey, true);
    document.addEventListener("keyup", onKey, true);
    return () => {
      document.removeEventListener("keydown", onKey, true);
      document.removeEventListener("keyup", onKey, true);
    };
  }, []);

  const percent = total > 0 ? (current / total) * 100 : 0;
  const controlClass =
    "grid h-7 min-w-7 shrink-0 place-items-center rounded-lg px-1 transition-colors hover:bg-white/15 focus-visible:ring-2 focus-visible:ring-white/70 focus-visible:outline-none active:scale-95";

  return (
    <div className={cn("size-full", className)} onPointerMove={wake}>
      <video
        ref={videoRef}
        playsInline
        loop={loop}
        preload="auto"
        onClick={() => {
          togglePlay();
          wake();
        }}
        onLoadedMetadata={(event) => {
          const { videoWidth, videoHeight } = event.currentTarget;
          if (videoWidth && videoHeight) onSize?.(videoWidth, videoHeight);
        }}
        onError={onError}
        className="size-full cursor-pointer object-contain"
      />
      <div
        className={cn(
          "absolute inset-x-0 bottom-0 flex flex-wrap items-center gap-x-3 gap-y-1 bg-gradient-to-t from-black/55 to-transparent px-4 pt-10 pb-3.5 text-[13px] text-white transition-opacity",
          paused || !idle ? "opacity-100" : "pointer-events-none opacity-0",
        )}
        style={{ transitionDuration: `${DURATION.fast * 1000}ms` }}
      >
        <button
          type="button"
          aria-label={paused ? "播放（空格）" : "暂停（空格）"}
          onClick={togglePlay}
          className={controlClass}
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
          onChange={(event) => {
            const video = videoRef.current;
            if (video) video.currentTime = Number(event.target.value);
          }}
          style={{
            background: `linear-gradient(to right, #fff ${percent}%, rgb(255 255 255 / 0.35) ${percent}%)`,
          }}
          className="h-0.5 min-w-32 flex-1 cursor-pointer appearance-none rounded-full [&::-moz-range-thumb]:size-2.5 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:bg-white [&::-webkit-slider-thumb]:size-2.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-white"
        />
        <span className="shrink-0 tabular-nums text-white/90">
          {clock(current)} / {clock(total)}
        </span>
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label="倍速"
            className={cn(controlClass, "px-2 text-[13px] tabular-nums")}
          >
            {speed}x
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" align="end" className="w-24 min-w-24">
            <DropdownMenuRadioGroup
              value={String(speed)}
              onValueChange={(value) => {
                const next = Number(value);
                setSpeed(next);
                if (videoRef.current) videoRef.current.playbackRate = next;
              }}
            >
              {SPEEDS.map((value) => (
                <DropdownMenuRadioItem key={value} value={String(value)} className="tabular-nums">
                  {value}x
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <button
          type="button"
          aria-label="循环播放"
          aria-pressed={loop}
          onClick={() => setLoop((value) => !value)}
          className={cn(controlClass, loop && "bg-white/25")}
        >
          <Repeat className="size-4" />
        </button>
        <button
          type="button"
          aria-label={muted ? "取消静音（M）" : "静音（M）"}
          onClick={toggleMuted}
          className={controlClass}
        >
          {muted ? <VolumeX className="size-[18px]" /> : <Volume2 className="size-[18px]" />}
        </button>
      </div>
    </div>
  );
}
