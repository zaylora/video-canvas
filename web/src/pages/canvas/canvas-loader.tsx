import { useCallback, useEffect, useRef, useState } from "react";

/**
 * 加载层至少停留多久：等 logo 聚焦完再退场，退场从清晰的样子接着走，不会跳一下。
 * 和 index.css 里 canvas-focus-in 的 0.9s 对齐。
 */
const MIN_SHOW_MS = 900;
/** 退场（logo 失焦 + 整层淡出）的总时长，到点兜底收尾 */
const EXIT_FALLBACK_MS = 700;

/**
 * 进入画布前的加载层（设计稿原型 A「聚焦显影」）：
 * 点阵底上，白色取景框 logo 和产品名由虚到实聚焦出来；
 * 画布数据到了（ready）且至少停留了 MIN_SHOW_MS，logo 再虚掉缩小、整层淡出，画布内容随之入场。
 * 动画全在 index.css 里用 CSS 关键帧做，这里只切 data-leaving，
 * 所以画布在下面挂载、占住主线程时动画照样流畅。
 * onOpen 在开始退场那一刻调用（画布内容开始入场），onDone 在整层淡完时调用（卸掉加载层）。
 */
export function CanvasLoader({
  title,
  ready,
  onOpen,
  onDone,
}: {
  title?: string;
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
    // animationend 偶尔收不到（标签页在后台等），到点兜底收尾
    const timer = window.setTimeout(finish, EXIT_FALLBACK_MS);
    return () => window.clearTimeout(timer);
  }, [finish, leaving, onOpen]);

  return (
    <div
      role="status"
      aria-live="polite"
      aria-label={title ? `正在打开画布「${title}」` : "正在打开画布"}
      data-leaving={leaving || undefined}
      onAnimationEnd={(event) => {
        if (event.target === event.currentTarget && event.animationName === "canvas-loader-out")
          finish();
      }}
      className="canvas-loader fixed inset-0 z-50 grid place-content-center justify-items-center gap-5"
    >
      <div className="canvas-loader-focus bg-foreground text-background grid size-18 place-items-center rounded-[22px]">
        <svg viewBox="0 0 16 16" fill="none" className="size-10" aria-hidden>
          <rect x="2.5" y="4" width="8" height="8" rx="2" stroke="currentColor" strokeWidth="1.6" />
          <path d="M10.5 7.2 13.5 5.5v5l-3-1.7" fill="currentColor" />
        </svg>
      </div>
      <div data-later className="canvas-loader-focus grid justify-items-center gap-1.5 text-center">
        <span className="text-[15px] font-semibold tracking-wide">Video Canvas</span>
        <span className="text-muted-foreground max-w-[28ch] truncate text-xs">
          {title ? `正在打开「${title}」` : "正在打开画布"}
        </span>
      </div>
    </div>
  );
}
