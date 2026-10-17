import { useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Camera, Loader2, Pause, Play, X } from "lucide-react";
import { toast } from "sonner";

import { ChromeButton, ChromeSeparator, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Filmstrip } from "@/components/canvas/trim-panel";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useFramePickStore, type StagedFrame } from "@/store/frame-pick";
import {
  canStageFrame,
  formatFrameTime,
  frameFileName,
  MAX_STAGED_FRAMES,
  stepFrameTime,
} from "@/utils/canvas/frame";
import { grabFrame } from "@/utils/canvas/frame-run";
import { frameEngine } from "@/utils/canvas/trim-engine";

/**
 * 一张暂存帧的小块：悬浮或键盘聚焦时，大图从小块上方弹出（先小一点、往下沉，再升到位），
 * 移开时更快地收回去。只动 transform 和 opacity，原点在小块正上方，看起来是从小块里「长」出来的。
 */
function StagedThumb({ frame }: { frame: StagedFrame }) {
  const [open, setOpen] = useState(false);
  return (
    <div
      className="relative"
      onPointerEnter={() => setOpen(true)}
      onPointerLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
    >
      <img
        src={frame.url}
        alt={`#${frame.id}`}
        draggable={false}
        className="ring-chrome-border size-10 rounded-lg object-cover ring-1"
      />
      <span className="pointer-events-none absolute bottom-0.5 left-1 text-[11px] font-medium text-white drop-shadow">
        #{frame.id}
      </span>
      <button
        type="button"
        aria-label={`丢弃 #${frame.id}`}
        onClick={() => useFramePickStore.getState().remove(frame.id)}
        className={cn(
          "bg-background text-foreground absolute -top-1.5 -right-1.5 grid size-4 place-items-center rounded-full shadow transition-opacity duration-150",
          open ? "opacity-100" : "pointer-events-none opacity-0 focus-visible:opacity-100",
        )}
      >
        <X className="size-3" />
      </button>
      <AnimatePresence>
        {open && (
          <motion.div
            key="preview"
            className="pointer-events-none absolute bottom-full left-1/2 z-20 pb-2"
            // 水平居中靠 x，缩放原点放在底边中点：看起来是从小块里「长」出来的
            style={{ x: "-50%", originX: 0.5, originY: 1 }}
            initial={{ opacity: 0, y: 14, scale: 0.92 }}
            animate={{
              opacity: 1,
              y: 0,
              scale: 1,
              transition: { duration: DURATION.slow, ease: EASE_OUT },
            }}
            exit={{
              opacity: 0,
              y: 8,
              scale: 0.96,
              transition: { duration: DURATION.exit, ease: EASE_OUT },
            }}
          >
            <div className="relative w-56 overflow-hidden rounded-2xl bg-black shadow-xl">
              <img src={frame.url} alt="" className="w-full object-cover" />
              <span className="absolute top-2 left-3 text-sm font-medium text-white drop-shadow">
                {formatFrameTime(frame.time)} 截取帧
              </span>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

/**
 * 选帧面板（参考图：胶片条加播放头，下排播放键和时间、暂存的帧、相机、确认）。
 * 样式和修剪面板同一套：半透明胶囊、同一条胶片条；没有手柄，只有一根白色播放头，
 * 点击或拖动胶片条定位，方向键走一帧、Shift 走一秒。截取只是暂存，点「确认」才一次性建节点。
 * 播放头、播放状态、暂存的帧都在 useFramePickStore 里，面板只管画。
 */
export function FramePanel({
  width,
  src,
  label,
  thumbs,
  loadError,
  onConfirm,
  onClose,
}: {
  width: number;
  /** 源视频地址 */
  src: string;
  /** 源视频节点标题，给截出的图片起名 */
  label: string;
  thumbs: (string | null)[];
  /** 读源视频失败的原因；有它时不能截取 */
  loadError?: string;
  /** 点确认：把暂存的文件交出去建节点 */
  onConfirm: (files: File[]) => void;
  /** 点 ×：退出选帧，暂存的帧丢弃 */
  onClose: () => void;
}) {
  const duration = useFramePickStore((state) => state.duration);
  const current = useFramePickStore((state) => state.current);
  const playing = useFramePickStore((state) => state.playing);
  const staged = useFramePickStore((state) => state.staged);
  const trackRef = useRef<HTMLDivElement | null>(null);
  const [busy, setBusy] = useState(false);

  const ready = duration > 0 && !loadError;
  // 只等时长：缩略图没好时胶片条自己有扫光占位，不挡着用户播放和截取
  const loading = !loadError && duration === 0;
  const total = duration || 1;

  /** 指针横坐标换成视频时刻 */
  const timeAt = (clientX: number) => {
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) return 0;
    return Math.min(Math.max(((clientX - rect.left) / rect.width) * duration, 0), duration);
  };

  const goTo = (time: number) => {
    const store = useFramePickStore.getState();
    store.setCurrent(time);
    store.seekTo(time);
  };

  const beginDrag = (event: PointerEvent<HTMLDivElement>) => {
    if (!ready || event.button !== 0) return;
    event.preventDefault();
    const target = event.currentTarget;
    target.setPointerCapture(event.pointerId);
    target.focus();
    goTo(timeAt(event.clientX));
    const onMove = (e: globalThis.PointerEvent) => goTo(timeAt(e.clientX));
    const onUp = (e: globalThis.PointerEvent) => {
      target.releasePointerCapture(e.pointerId);
      target.removeEventListener("pointermove", onMove);
      target.removeEventListener("pointerup", onUp);
      target.removeEventListener("pointercancel", onUp);
    };
    target.addEventListener("pointermove", onMove);
    target.addEventListener("pointerup", onUp);
    target.addEventListener("pointercancel", onUp);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!ready) return;
    const direction = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    if (direction === 0) return;
    event.preventDefault();
    goTo(stepFrameTime(current, direction, event.shiftKey, duration));
  };

  /** 点相机：把当前时刻的帧截下来暂存；到上限或重复时说明原因 */
  const capture = async () => {
    if (!ready || busy) return;
    const time = Math.round(useFramePickStore.getState().current * 100) / 100;
    const check = canStageFrame(staged, time);
    if (!check.ok) {
      toast(check.reason === "limit" ? `最多暂存 ${MAX_STAGED_FRAMES} 帧` : "这一帧已经截过了");
      return;
    }
    useFramePickStore.getState().setPlaying(false);
    setBusy(true);
    try {
      const file = await grabFrame(frameEngine, {
        src,
        spec: time,
        fileName: frameFileName(label, time),
      });
      useFramePickStore.getState().stage({ time, file, url: URL.createObjectURL(file) });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "截取失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="nodrag nopan nowheel bg-chrome text-foreground ring-chrome-border flex flex-col gap-3 rounded-2xl p-3 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150"
      style={{ width }}
      role="group"
      aria-label="截取帧"
    >
      <div
        ref={trackRef}
        role="slider"
        tabIndex={ready ? 0 : -1}
        aria-label="播放位置"
        aria-valuemin={0}
        aria-valuemax={duration}
        aria-valuenow={current}
        aria-valuetext={formatFrameTime(current)}
        aria-disabled={!ready}
        onPointerDown={beginDrag}
        onKeyDown={onKeyDown}
        className={cn(
          "focus-visible:ring-node-ring/60 relative h-12 w-full touch-none rounded-md outline-none select-none focus-visible:ring-2",
          ready ? "cursor-pointer" : "cursor-default",
        )}
      >
        <Filmstrip thumbs={thumbs} />
        <div
          className="pointer-events-none absolute inset-y-[-2px] w-0.5 -translate-x-1/2 rounded-full bg-white shadow"
          style={{ left: `${(Math.min(current, duration) / total) * 100}%` }}
        />
        {/* 读时长期间：胶片条正中的胶囊，和修剪面板的时长胶囊同一个位置、同一个样式 */}
        {loading && (
          <div className="pointer-events-none absolute inset-0 grid place-items-center">
            <span className="rounded-full bg-black/70 px-3 py-1 font-mono text-[13px] font-semibold tabular-nums text-white">
              加载中…
            </span>
          </div>
        )}
      </div>

      <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-3">
        <div className="flex items-center gap-2">
          <ChromeTooltip label="关闭">
            <ChromeButton aria-label="关闭截取帧" onClick={onClose}>
              <X />
            </ChromeButton>
          </ChromeTooltip>
          <ChromeSeparator />
          <ChromeButton
            aria-label={playing ? "暂停" : "播放"}
            disabled={!ready}
            onClick={() => useFramePickStore.getState().setPlaying(!playing)}
          >
            {playing ? <Pause className="fill-current" /> : <Play className="fill-current" />}
          </ChromeButton>
          <span className="font-mono text-sm tabular-nums">
            {formatFrameTime(current)} / {formatFrameTime(duration)}
          </span>
        </div>

        <div className="flex items-center gap-2">
          {staged.map((frame) => (
            <StagedThumb key={frame.id} frame={frame} />
          ))}
          <ChromeTooltip label={loadError ?? "截取当前画面"}>
            <span className="inline-flex">
              <ChromeButton
                aria-label="截取帧"
                disabled={!ready || busy}
                onClick={() => void capture()}
                className="bg-chrome-hover text-foreground h-10 rounded-xl px-4 text-sm"
              >
                {busy ? <Loader2 className="animate-spin" /> : <Camera />}
                {staged.length === 0 && <span>截取帧</span>}
              </ChromeButton>
            </span>
          </ChromeTooltip>
        </div>

        <div className="flex justify-end">
          <ChromeButton
            variant="primary"
            aria-label="确认"
            disabled={staged.length === 0}
            onClick={() => onConfirm(staged.map((frame) => frame.file))}
            className="h-10 w-auto rounded-xl px-6 text-sm font-semibold"
          >
            确认
          </ChromeButton>
        </div>
      </div>

      {loadError && (
        <p role="alert" className="text-destructive text-center text-xs">
          {loadError}
        </p>
      )}
    </div>
  );
}
