import { useEffect, useRef, useState } from "react";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  ChevronLeft,
  ChevronRight,
  Crosshair,
  Download,
  ImageOff,
  Play,
  RotateCcw,
  X,
} from "lucide-react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { DURATION, EASE_OUT, SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { downloadMedia, downloadName } from "@/utils/canvas/download";
import { variantUrl } from "@/utils/canvas/media-lod";
import {
  formatFileSize,
  resolveActiveIndex,
  stepPreviewIndex,
  type PreviewItem,
} from "@/utils/canvas/preview-items";

import { LightboxVideo } from "./lightbox-video";
import { MediaPreview, VideoPoster } from "./media-preview";

/** 预览的目标：看哪个节点，以及弹层从屏幕哪个点长出来 */
export type LightboxTarget = {
  id: string;
  /** 被双击节点在屏幕上的中心，弹层的缩放原点；没有就从中间出来 */
  origin: { x: number; y: number } | null;
};

type MediaLightboxProps = {
  /** 当前画布上全部可预览项，顺序即翻页顺序 */
  items: PreviewItem[];
  /** 正在看的目标；null 表示关着 */
  target: LightboxTarget | null;
  /** 翻页后回调 */
  onActiveChange: (id: string) => void;
  /** 「定位到节点」：弹层已请求关闭，调用方负责选中并飞过去 */
  onLocate: (id: string) => void;
  onClose: () => void;
};

/** 没读到画幅前先按 16:9 摆，免得舞台从零开始撑开 */
const DEFAULT_RATIO = 16 / 9;

/** 图标按钮的统一外观：浮条底色，不加 backdrop-blur（同屏模糊层有上限，只留遮罩那一层） */
const CIRCLE_BUTTON =
  "bg-chrome ring-chrome-border text-foreground hover:bg-chrome-hover grid size-10 shrink-0 place-items-center rounded-full ring-1 transition-[color,background-color,transform] outline-none focus-visible:ring-node-ring focus-visible:ring-2 active:scale-[0.96] disabled:pointer-events-none disabled:opacity-30";

/** 读原文件大小：HEAD 请求的 Content-Length，拿不到（跨域、被压缩）就是 null，信息胶囊只显示宽高 */
function useFileSize(src: string | undefined): number | null {
  const [state, setState] = useState<{ src: string; bytes: number } | null>(null);
  useEffect(() => {
    if (!src) return;
    const controller = new AbortController();
    fetch(src, { method: "HEAD", signal: controller.signal })
      .then((response) => {
        const bytes = Number(response.headers.get("content-length"));
        if (response.ok && bytes > 0) setState({ src, bytes });
      })
      .catch(() => {});
    return () => controller.abort();
  }, [src]);
  return state && state.src === src ? state.bytes : null;
}

type StageProps = {
  item: PreviewItem;
  /** 读到画幅（缩略图、原图、视频任一先到）就回调，舞台据此贴合画面 */
  onRatio: (width: number, height: number) => void;
  /** 读到原图 / 视频的真实尺寸 */
  onNatural: (width: number, height: number) => void;
};

/**
 * 图片舞台：先铺缩略图（节点里多半已缓存），原图加载完成再淡入盖上去，避免黑屏。
 * 原图失败给占位、重试和下载入口，缩略图失败直接看原图。
 */
function ImageStage({ item, onRatio, onNatural }: StageProps) {
  const reduce = useReducedMotion();
  const thumb = variantUrl(item.src, "thumb");
  const [thumbFailed, setThumbFailed] = useState(false);
  const [state, setState] = useState<"pending" | "loaded" | "failed">("pending");
  const [attempt, setAttempt] = useState(0);
  const showThumb = thumb.hasVariant && !thumbFailed;

  if (state === "failed") {
    return (
      <div
        role="img"
        aria-label="原图加载失败"
        className="text-muted-foreground bg-card flex size-full flex-col items-center justify-center gap-3 text-sm"
      >
        <ImageOff className="size-8 opacity-60" />
        原图加载失败
        <button
          type="button"
          onClick={() => {
            setState("pending");
            setAttempt((value) => value + 1);
          }}
          className="ring-chrome-border hover:bg-chrome-hover flex h-8 items-center gap-1.5 rounded-full px-3 text-xs ring-1 transition-colors"
        >
          <RotateCcw className="size-3.5" />
          重试
        </button>
      </div>
    );
  }

  return (
    <div className="relative size-full">
      {showThumb && (
        <img
          src={thumb.url}
          alt=""
          draggable={false}
          onLoad={(event) =>
            onRatio(event.currentTarget.naturalWidth, event.currentTarget.naturalHeight)
          }
          onError={() => setThumbFailed(true)}
          className="absolute inset-0 size-full object-contain"
        />
      )}
      <motion.img
        key={attempt}
        src={item.src}
        alt={item.label}
        draggable={false}
        initial={false}
        animate={{ opacity: state === "loaded" || !showThumb ? 1 : 0 }}
        transition={{ duration: reduce ? 0 : DURATION.base, ease: EASE_OUT }}
        onLoad={(event) => {
          const { naturalWidth, naturalHeight } = event.currentTarget;
          setState("loaded");
          onRatio(naturalWidth, naturalHeight);
          onNatural(naturalWidth, naturalHeight);
        }}
        onError={() => setState("failed")}
        className="absolute inset-0 size-full object-contain"
      />
    </div>
  );
}

/** 视频舞台：先露封面占位，视频能播后接管；无法播放给说明 */
function VideoStage({ item, onRatio, onNatural }: StageProps) {
  const poster = variantUrl(item.src, "poster");
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);

  if (failed) {
    return (
      <div
        role="alert"
        className="text-muted-foreground bg-card flex size-full flex-col items-center justify-center gap-3 text-sm"
      >
        <ImageOff className="size-8 opacity-60" />
        视频无法播放
        <button
          type="button"
          onClick={() => {
            setFailed(false);
            setAttempt((value) => value + 1);
          }}
          className="ring-chrome-border hover:bg-chrome-hover flex h-8 items-center gap-1.5 rounded-full px-3 text-xs ring-1 transition-colors"
        >
          <RotateCcw className="size-3.5" />
          重试
        </button>
      </div>
    );
  }

  return (
    <div className="relative size-full bg-black">
      {poster.hasVariant && (
        <img
          src={poster.url}
          alt=""
          draggable={false}
          onLoad={(event) =>
            onRatio(event.currentTarget.naturalWidth, event.currentTarget.naturalHeight)
          }
          className="absolute inset-0 size-full object-contain"
        />
      )}
      <LightboxVideo
        key={attempt}
        src={item.src}
        className="absolute inset-0"
        onSize={(width, height) => {
          onRatio(width, height);
          onNatural(width, height);
        }}
        onError={() => setFailed(true)}
      />
    </div>
  );
}

/** 带快捷键提示的图标按钮外壳，按下缩到 0.96（和 TAP 一致） */
function TipButton({ tip, children, ...props }: React.ComponentProps<"button"> & { tip: string }) {
  return (
    <Tooltip>
      <TooltipTrigger render={<button type="button" {...props} />}>{children}</TooltipTrigger>
      <TooltipContent>{tip}</TooltipContent>
    </Tooltip>
  );
}

/**
 * 双击节点后的媒体预览弹层（设计稿 6.9）：原图 / 原视频居中放大，
 * 底部是整张画布可预览节点的缩略图条，←/→ 或点缩略图连续翻看，「定位到节点」回到画布。
 * 受控组件：弹层状态在调用方，这里只管长相、键盘和动效。
 * 打开和关闭用 CSS 过渡，由 Base UI 在过渡结束后才卸载，所以关闭时也有出场动画。
 */
export function MediaLightbox({
  items,
  target,
  onActiveChange,
  onLocate,
  onClose,
}: MediaLightboxProps) {
  const reduce = useReducedMotion();
  const open = target !== null;
  const popupRef = useRef<HTMLDivElement>(null);

  // 关闭过程中还要继续画着最后那一帧，所以把最近一次的目标留下来
  const [last, setLast] = useState(target);
  if (target && target !== last) setLast(target);
  const shown = target ?? last;

  // 记住上一次的位置：当前项被删后要落到原位置上的相邻项
  const [lastIndex, setLastIndex] = useState(0);
  const index = shown ? resolveActiveIndex(items, shown.id, lastIndex) : -1;
  const item = index >= 0 ? items[index] : undefined;
  if (index >= 0 && index !== lastIndex) setLastIndex(index);

  // 当前项被删、素材被换掉：落到相邻项；一个都不剩就关
  useEffect(() => {
    if (!target) return;
    if (!item) onClose();
    else if (item.id !== target.id) onActiveChange(item.id);
  }, [item, onActiveChange, onClose, target]);

  const [ratios, setRatios] = useState<Record<string, number>>({});
  const [naturals, setNaturals] = useState<Record<string, { w: number; h: number }>>({});
  const bytes = useFileSize(open ? item?.src : undefined);

  /** 翻页方向，决定新画面从哪边滑进来 */
  const [direction, setDirection] = useState<1 | -1>(1);
  const goTo = (next: number) => {
    const nextItem = items[next];
    if (!nextItem || next === index) return;
    setDirection(next > index ? 1 : -1);
    onActiveChange(nextItem.id);
  };
  const step = (delta: -1 | 1) => goTo(stepPreviewIndex(index, delta, items.length));

  useEffect(() => {
    if (!open) return;
    /*
     * 捕获阶段处理：焦点停在进度条、按钮上时方向键会被它们先吃掉。
     * 弹层里 ←/→ 就是切换节点，进度条也不例外（进度靠点击或拖动）
     */
    const onKey = (event: KeyboardEvent) => {
      if ((event.target as HTMLElement | null)?.closest("[role='menu']")) return;
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      step(event.key === "ArrowLeft" ? -1 : 1);
    };
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  });

  // 当前项变了就把缩略图条滚到它身上
  const activeThumbRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    activeThumbRef.current?.scrollIntoView({
      inline: "center",
      block: "nearest",
      behavior: reduce ? "auto" : "smooth",
    });
  }, [item?.id, open, reduce]);

  const ratio = (item && ratios[item.src]) || DEFAULT_RATIO;
  const natural = item ? naturals[item.src] : undefined;
  const sizeText = formatFileSize(bytes);
  const meta = natural
    ? `${natural.w}×${natural.h}${sizeText ? ` · ${sizeText}` : ""}`
    : item?.mediaType === "video"
      ? "加载视频…"
      : "加载原图…";
  const many = items.length > 1;
  const origin = shown?.origin;

  return (
    <DialogPrimitive.Root open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Popup
          ref={popupRef}
          // 焦点落在弹层本身，而不是第一个按钮：这样一打开就能直接用键盘，也不会误触按钮
          initialFocus={popupRef}
          aria-label="媒体预览"
          data-slot="media-lightbox"
          // 点空白处关闭：舞台层 pointer-events-none，穿透下来的点击落在这里
          onClick={(event) => {
            if (event.target === event.currentTarget) onClose();
          }}
          className={cn(
            "group/lb fixed inset-0 z-50 outline-none",
            "transition-opacity duration-[240ms] ease-[cubic-bezier(0.2,0,0,1)]",
            // 退出约为进入的 70%
            "data-ending-style:duration-[170ms] data-ending-style:opacity-0 data-starting-style:opacity-0",
          )}
        >
          <div
            aria-hidden
            className="bg-cover-scrim absolute inset-0 backdrop-blur-sm"
            onClick={onClose}
          />

          {/* 舞台层：从被双击的节点长出来，只做 transform 与 opacity */}
          <div
            className={cn(
              "pointer-events-none absolute inset-0 flex items-center justify-center px-2 pt-[72px] pb-[150px] sm:px-20",
              "transition-transform duration-[240ms] ease-[cubic-bezier(0.2,0,0,1)]",
              "group-data-ending-style/lb:duration-[170ms] group-data-ending-style/lb:scale-[0.96] group-data-starting-style/lb:scale-[0.96]",
              "motion-reduce:transform-none motion-reduce:transition-none",
            )}
            style={{ transformOrigin: origin ? `${origin.x}px ${origin.y}px` : "50% 50%" }}
          >
            <div className="relative flex size-full items-center justify-center">
              <AnimatePresence initial={false} mode="popLayout">
                {item && (
                  <motion.div
                    key={item.id}
                    initial={{ opacity: 0, x: reduce ? 0 : direction * 12 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: DURATION.base, ease: EASE_OUT }}
                    className="pointer-events-auto relative overflow-hidden rounded-2xl bg-black shadow-2xl"
                    style={{
                      aspectRatio: ratio,
                      // 按画幅贴合画面：宽取「可用宽度」与「可用高度换算出的宽度」的较小者
                      width: `min(100%, calc((100svh - 222px) * ${ratio}))`,
                    }}
                  >
                    {item.mediaType === "image" ? (
                      <ImageStage
                        item={item}
                        onRatio={(w, h) => setRatios((prev) => ({ ...prev, [item.src]: w / h }))}
                        onNatural={(w, h) =>
                          setNaturals((prev) => ({ ...prev, [item.src]: { w, h } }))
                        }
                      />
                    ) : (
                      <VideoStage
                        item={item}
                        onRatio={(w, h) => setRatios((prev) => ({ ...prev, [item.src]: w / h }))}
                        onNatural={(w, h) =>
                          setNaturals((prev) => ({ ...prev, [item.src]: { w, h } }))
                        }
                      />
                    )}
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          </div>

          {/* 顶部右：信息胶囊 / 下载 / 关闭 */}
          <div className="absolute top-5 right-4 flex items-center gap-2 sm:right-6">
            <span
              className="bg-chrome ring-chrome-border text-foreground flex h-10 items-center rounded-full px-3.5 text-sm tabular-nums ring-1"
              aria-live="polite"
            >
              {meta}
            </span>
            <TipButton
              tip="下载原文件"
              aria-label="下载原文件"
              disabled={!item}
              className={CIRCLE_BUTTON}
              onClick={() =>
                item &&
                void downloadMedia(item.src, downloadName(item.label, item.src, item.fileName))
              }
            >
              <Download className="size-[18px]" />
            </TipButton>
            <TipButton
              tip="关闭（Esc）"
              aria-label="关闭"
              className={CIRCLE_BUTTON}
              onClick={onClose}
            >
              <X className="size-[18px]" />
            </TipButton>
          </div>

          {/* 左右切换：只有一项时不出现 */}
          {many && (
            <>
              <TipButton
                tip="上一个（←）"
                aria-label="上一个"
                disabled={index <= 0}
                onClick={() => step(-1)}
                className={cn(
                  CIRCLE_BUTTON,
                  "absolute top-[calc(72px+(100svh-222px)/2)] left-1 size-9 -translate-y-1/2 sm:left-4 sm:size-11",
                )}
              >
                <ChevronLeft className="size-5" />
              </TipButton>
              <TipButton
                tip="下一个（→）"
                aria-label="下一个"
                disabled={index >= items.length - 1}
                onClick={() => step(1)}
                className={cn(
                  CIRCLE_BUTTON,
                  "absolute top-[calc(72px+(100svh-222px)/2)] right-1 size-9 -translate-y-1/2 sm:right-4 sm:size-11",
                )}
              >
                <ChevronRight className="size-5" />
              </TipButton>
            </>
          )}

          {/* 底部：信息行 + 缩略图条 */}
          <div className="absolute inset-x-0 bottom-5 flex flex-col items-center gap-3">
            <div className="text-muted-foreground flex items-center gap-2 text-xs">
              {many && (
                <span className="bg-chrome ring-chrome-border flex h-7 items-center rounded-full px-3 tabular-nums ring-1">
                  相邻节点 {index + 1} / {items.length}
                </span>
              )}
              {many && (
                <span className="bg-chrome ring-chrome-border hidden h-7 items-center gap-1.5 rounded-full px-3 ring-1 sm:flex">
                  <kbd className="bg-chrome-hover rounded px-1.5">←</kbd>
                  <kbd className="bg-chrome-hover rounded px-1.5">→</kbd>
                  切换
                </span>
              )}
              <motion.button
                type="button"
                whileTap={TAP}
                disabled={!item}
                onClick={() => item && onLocate(item.id)}
                className="bg-chrome ring-chrome-border text-foreground hover:bg-chrome-hover flex h-7 items-center gap-1.5 rounded-full px-3 ring-1 transition-colors outline-none focus-visible:ring-node-ring focus-visible:ring-2"
              >
                <Crosshair className="size-3.5" />
                定位到节点
              </motion.button>
            </div>
            {many && (
              <div className="flex [scrollbar-width:none] [&::-webkit-scrollbar]:hidden max-w-[calc(100vw-32px)] gap-2 overflow-x-auto p-1">
                {items.map((entry, i) => {
                  const active = i === index;
                  return (
                    <button
                      key={entry.id}
                      ref={active ? activeThumbRef : undefined}
                      type="button"
                      aria-label={entry.label}
                      aria-current={active}
                      onClick={() => goTo(i)}
                      className={cn(
                        "bg-card relative aspect-video h-14 shrink-0 overflow-hidden rounded-[10px] transition-opacity outline-none focus-visible:ring-node-ring focus-visible:ring-2",
                        active ? "opacity-100" : "opacity-70 hover:opacity-100",
                      )}
                    >
                      {entry.mediaType === "image" ? (
                        <MediaPreview src={entry.src} mode="thumb" fit="cover" draggable={false} />
                      ) : (
                        <>
                          <VideoPoster src={entry.src} iconClassName="size-4" />
                          <span className="pointer-events-none absolute inset-0 grid place-items-center">
                            <span className="grid size-5 place-items-center rounded-full bg-black/50 text-white">
                              <Play className="size-2.5 translate-x-px fill-current" />
                            </span>
                          </span>
                        </>
                      )}
                      {active && (
                        <motion.span
                          layoutId="lightbox-active-thumb"
                          transition={reduce ? { duration: 0 } : SPRING}
                          className="ring-node-ring pointer-events-none absolute inset-0 rounded-[inherit] ring-2 ring-inset"
                        />
                      )}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </DialogPrimitive.Popup>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
