import { useRef, type KeyboardEvent, type PointerEvent } from "react";
import { Check, Loader2, Volume2, VolumeX, X } from "lucide-react";

import {
  ChromeButton,
  ChromePill,
  ChromeSeparator,
  ChromeTooltip,
} from "@/components/canvas/chrome/chrome";
import { cn } from "@/lib/utils";
import { useTrimStore } from "@/store/trim";
import {
  TRIM_MIN_GAP,
  canConfirmTrim,
  clampTrimTime,
  confirmHint,
  formatTrimSeconds,
  trimRange,
  type TrimEnd,
} from "@/utils/canvas/trim";

/** 手柄的键盘微调步长（秒）；按住 Shift 走大步 */
const KEY_STEP = 0.1;
const KEY_STEP_BIG = 1;

/** 胶片条的缩略图：失败的格子用灰底占位 */
function Filmstrip({ thumbs }: { thumbs: (string | null)[] }) {
  const cells = thumbs.length ? thumbs : Array.from({ length: 16 }, () => null);
  return (
    <div className="absolute inset-0 flex overflow-hidden rounded-md" aria-hidden>
      {cells.map((url, index) => (
        <div key={index} className="bg-muted/60 relative h-full min-w-0 flex-1">
          {url && <img src={url} alt="" draggable={false} className="size-full object-cover" />}
        </div>
      ))}
    </div>
  );
}

/** 一个手柄：8px 宽的蓝条，点击热区 24px；role=slider 可聚焦、可键盘微调 */
function Handle({
  which,
  value,
  min,
  max,
  disabled,
  percent,
  onPointerDown,
  onKeyDown,
}: {
  which: TrimEnd;
  value: number;
  min: number;
  max: number;
  disabled: boolean;
  percent: number;
  onPointerDown: (event: PointerEvent<HTMLDivElement>) => void;
  onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void;
}) {
  return (
    <div
      role="slider"
      tabIndex={disabled ? -1 : 0}
      aria-label={which === "start" ? "入点" : "出点"}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={value}
      aria-valuetext={formatTrimSeconds(value)}
      aria-disabled={disabled}
      data-trim-handle={which}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      className={cn(
        "nodrag nopan absolute top-0 z-10 flex h-full w-6 -translate-x-1/2 touch-none items-stretch justify-center outline-none",
        disabled ? "cursor-default" : "cursor-ew-resize",
      )}
      style={{ left: `${percent}%` }}
    >
      <span
        className={cn(
          "w-2 rounded-sm bg-blue-500 shadow-sm transition-colors",
          "focus-visible:ring-node-ring/60",
          disabled && "opacity-60",
        )}
      />
    </div>
  );
}

/**
 * 修剪面板（设计稿「视频修剪」§6.3，参考图 1 与图 3）：
 * 左 × 关闭；中间胶片条（缩略图 + 两端蓝色手柄 + 居中时长胶囊 + 白色播放头）；
 * 右 去音轨、✓ 确认。编码中手柄锁定、✓ 变成转圈，面板下方出现进度条「编码中 x%」。
 * 选区、去音轨、进度全在 useTrimStore 里，面板卸载（取消选中）后重新挂上能接着显示。
 */
export function TrimPanel({
  width,
  thumbs,
  loadError,
  onConfirm,
  onClose,
}: {
  width: number;
  thumbs: (string | null)[];
  /** 读源视频失败的原因；有它时手柄仍可拖，但 ✓ 不可用 */
  loadError?: string;
  onConfirm: () => void;
  /** 点 ×：编辑态直接退出；编码中由调用方弹确认再中止 */
  onClose: () => void;
}) {
  const phase = useTrimStore((state) => state.phase);
  const start = useTrimStore((state) => state.start);
  const end = useTrimStore((state) => state.end);
  const duration = useTrimStore((state) => state.duration);
  const hasAudio = useTrimStore((state) => state.hasAudio);
  const discardAudio = useTrimStore((state) => state.discardAudio);
  const progress = useTrimStore((state) => state.progress);
  const error = useTrimStore((state) => state.error);
  const current = useTrimStore((state) => state.current);

  const trackRef = useRef<HTMLDivElement | null>(null);
  const locked = phase !== "editing";
  const encoding = phase === "encoding";
  const gate = { start, end, duration, phase };
  const canConfirm = canConfirmTrim(gate) && !loadError;
  const total = duration || 1;
  const pct = (time: number) => (time / total) * 100;

  /** 指针横坐标换成视频时刻 */
  const timeAt = (clientX: number) => {
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) return 0;
    return clampTrimTime(((clientX - rect.left) / rect.width) * duration, duration);
  };

  /** 改某一端：写仓库的选区，并让预览跳到那一端 */
  const move = (which: TrimEnd, value: number) => {
    const next = trimRange(start, end, duration, which, value);
    const store = useTrimStore.getState();
    store.setRange(next.start, next.end);
    store.seekTo(which === "start" ? next.start : next.end);
  };

  const beginDrag = (which: TrimEnd) => (event: PointerEvent<HTMLDivElement>) => {
    if (locked || event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const target = event.currentTarget;
    target.setPointerCapture(event.pointerId);
    target.focus();
    const onMove = (e: globalThis.PointerEvent) => {
      const latest = useTrimStore.getState();
      const next = trimRange(latest.start, latest.end, latest.duration, which, timeAt(e.clientX));
      latest.setRange(next.start, next.end);
      latest.seekTo(which === "start" ? next.start : next.end);
    };
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

  const keyDrag = (which: TrimEnd) => (event: KeyboardEvent<HTMLDivElement>) => {
    if (locked) return;
    const step = event.shiftKey ? KEY_STEP_BIG : KEY_STEP;
    const base = which === "start" ? start : end;
    if (event.key === "ArrowLeft") move(which, base - step);
    else if (event.key === "ArrowRight") move(which, base + step);
    else return;
    event.preventDefault();
  };

  /** 点胶片条空白：在选区内定位预览 */
  const seekInside = (event: PointerEvent<HTMLDivElement>) => {
    if (locked || (event.target as HTMLElement).closest("[data-trim-handle]")) return;
    const time = Math.min(Math.max(timeAt(event.clientX), start), end);
    useTrimStore.getState().seekTo(time);
  };

  const hint = loadError ?? confirmHint(gate);

  return (
    <div className="nodrag nopan nowheel flex flex-col gap-2" style={{ width }}>
      <ChromePill className="h-16 w-full gap-2 rounded-2xl px-2" aria-label="视频修剪">
        <ChromeTooltip label={encoding ? "中止编码" : "关闭"}>
          <ChromeButton aria-label={encoding ? "中止编码" : "关闭修剪"} onClick={onClose}>
            <X />
          </ChromeButton>
        </ChromeTooltip>
        <ChromeSeparator />

        <div
          ref={trackRef}
          onPointerDown={seekInside}
          className="relative mx-1.5 h-12 min-w-0 flex-1 select-none"
        >
          <Filmstrip thumbs={thumbs} />
          {/* 选区外压暗 */}
          <div
            className="pointer-events-none absolute inset-y-0 left-0 rounded-l-md bg-black/55"
            style={{ width: `${pct(start)}%` }}
          />
          <div
            className="pointer-events-none absolute inset-y-0 right-0 rounded-r-md bg-black/55"
            style={{ width: `${100 - pct(end)}%` }}
          />
          {/* 选区上下边框 */}
          <div
            className="pointer-events-none absolute inset-y-0 border-y-2 border-blue-500"
            style={{ left: `${pct(start)}%`, width: `${pct(end) - pct(start)}%` }}
          />
          {/* 播放头 */}
          <div
            className="pointer-events-none absolute inset-y-[-2px] w-0.5 -translate-x-1/2 rounded-full bg-white shadow"
            style={{ left: `${pct(current)}%` }}
          />
          {/* 居中的时长胶囊 */}
          <div className="pointer-events-none absolute inset-0 grid place-items-center">
            <span className="rounded-full bg-black/70 px-3 py-1 font-mono text-[13px] font-semibold tabular-nums text-white">
              {formatTrimSeconds(end - start)}
            </span>
          </div>
          <Handle
            which="start"
            value={start}
            min={0}
            max={Math.max(end - TRIM_MIN_GAP, 0)}
            percent={pct(start)}
            disabled={locked}
            onPointerDown={beginDrag("start")}
            onKeyDown={keyDrag("start")}
          />
          <Handle
            which="end"
            value={end}
            min={Math.min(start + TRIM_MIN_GAP, duration)}
            max={duration}
            percent={pct(end)}
            disabled={locked}
            onPointerDown={beginDrag("end")}
            onKeyDown={keyDrag("end")}
          />
        </div>

        <ChromeSeparator />
        <ChromeTooltip
          label={hasAudio ? (discardAudio ? "保留声音" : "去掉声音") : "该视频没有声音"}
        >
          <ChromeButton
            aria-label="去音轨"
            aria-pressed={discardAudio}
            disabled={locked || !hasAudio}
            active={discardAudio}
            onClick={() => useTrimStore.getState().setDiscardAudio(!discardAudio)}
          >
            {discardAudio || !hasAudio ? <VolumeX /> : <Volume2 />}
          </ChromeButton>
        </ChromeTooltip>
        <ChromeTooltip label={hint}>
          <span className="inline-flex">
            <ChromeButton
              variant="primary"
              size="lg"
              aria-label="确认修剪"
              disabled={!canConfirm}
              onClick={onConfirm}
              className="rounded-xl"
            >
              {encoding ? <Loader2 className="animate-spin" /> : <Check />}
            </ChromeButton>
          </span>
        </ChromeTooltip>
      </ChromePill>

      {encoding && (
        <div className="px-1" role="status" aria-live="polite">
          <div className="bg-muted h-0.5 w-full overflow-hidden rounded-full">
            <div
              className="bg-foreground h-full rounded-full transition-[width] duration-200 ease-out"
              style={{ width: `${progress}%` }}
            />
          </div>
          <p className="text-muted-foreground mt-1.5 text-center text-xs tabular-nums">
            编码中 {progress}%
          </p>
        </div>
      )}
      {!encoding && (error || loadError) && (
        <p role="alert" className="text-destructive px-2 text-center text-xs">
          {error ?? loadError}
        </p>
      )}
    </div>
  );
}
