import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

/**
 * 加载层至少停留多久：等 logo 聚焦完再退场，退场从清晰的样子接着走，不会跳一下。
 * 和 index.css 里 canvas-focus-in 的 0.9s 对齐。
 */
const MIN_SHOW_MS = 900;
/** 退场（logo 失焦 + 整层淡出）的总时长，到点兜底收尾 */
const EXIT_FALLBACK_MS = 700;

/** 加载进度条：细条加百分比；条用 transform 缩放，不触发布局，主线程忙的时候也顺 */
function LoadBar({ progress }: { progress: number }) {
  const value = Math.min(1, Math.max(0, progress));
  const percent = Math.round(value * 100);
  return (
    <div className="mt-2.5 flex items-center gap-2">
      <div
        role="progressbar"
        aria-label="加载进度"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        className="bg-foreground/15 h-0.5 w-32 overflow-hidden rounded-full"
      >
        <div
          className="bg-foreground h-full origin-left rounded-full transition-transform duration-300 ease-out"
          style={{ transform: `scaleX(${value})` }}
        />
      </div>
      <span className="text-muted-foreground w-8 text-left text-[11px] tabular-nums">
        {percent}%
      </span>
    </div>
  );
}

/**
 * 进入一个“空间”前的全屏加载层（设计稿原型 A「聚焦显影」）：进画布、进后台共用。
 * 点阵底上，白色图标块和标题由虚到实聚焦出来；
 * ready 且至少停留了 MIN_SHOW_MS 后，图标再虚掉缩小、整层淡出，下面的内容随之入场。
 * 动画全在 index.css 里用 CSS 关键帧做，这里只切 data-leaving，
 * 所以下面的页面挂载、占住主线程时动画照样流畅。
 * onOpen 在开始退场那一刻调用（内容开始入场），onDone 在整层淡完时调用（卸掉加载层）。
 */
export function FocusLoader({
  icon,
  plainIcon = false,
  title,
  subtitle,
  label,
  progress,
  ready,
  onOpen,
  onDone,
}: {
  /** 图标块里的图形，约 40px */
  icon: ReactNode;
  /** 图标自带进退场动画（连镜 Logo 的「开镜 · 对焦 · 光铺开」）：不套深色圆角块，也不做聚焦显影 */
  plainIcon?: boolean;
  /** 图标下方的主标题 */
  title: string;
  /** 主标题下方的灰色说明 */
  subtitle: string;
  /** 读屏播报的文字 */
  label: string;
  /** 加载进度（0 ~ 1）：给了才在说明下面画进度条和百分比；进画布、进后台不给 */
  progress?: number;
  ready: boolean;
  onOpen: () => void;
  onDone: () => void;
}) {
  const [minElapsed, setMinElapsed] = useState(false);
  const leaving = ready && minElapsed;
  const doneRef = useRef(false);

  useEffect(() => {
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const timer = window.setTimeout(() => setMinElapsed(true), reduce ? 0 : MIN_SHOW_MS);
    return () => window.clearTimeout(timer);
  }, []);

  const finish = useCallback(() => {
    if (doneRef.current) return;
    doneRef.current = true;
    onDone();
  }, [onDone]);

  useEffect(() => {
    if (!leaving) return;
    onOpen();
    /** animationend 偶尔收不到（标签页在后台等），到点兜底收尾 */
    const timer = window.setTimeout(finish, EXIT_FALLBACK_MS);
    return () => window.clearTimeout(timer);
  }, [finish, leaving, onOpen]);

  return (
    <div
      role="status"
      aria-live="polite"
      aria-label={label}
      data-leaving={leaving || undefined}
      onAnimationEnd={(event) => {
        if (event.target === event.currentTarget && event.animationName === "canvas-loader-out")
          finish();
      }}
      className="canvas-loader fixed inset-0 z-50 grid place-content-center justify-items-center gap-5"
    >
      {plainIcon ? (
        <div className="grid size-18 place-items-center">{icon}</div>
      ) : (
        <div className="canvas-loader-focus bg-foreground text-background grid size-18 place-items-center rounded-[22px]">
          {icon}
        </div>
      )}
      <div data-later className="canvas-loader-focus grid justify-items-center gap-1.5 text-center">
        <span className="text-[15px] font-semibold tracking-wide">{title}</span>
        <span className="text-muted-foreground max-w-[28ch] truncate text-xs">{subtitle}</span>
        {progress !== undefined && <LoadBar progress={progress} />}
      </div>
    </div>
  );
}
