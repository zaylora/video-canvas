import { useCallback, useEffect, useRef, useState } from "react";
import { Camera, Loader2, Pause, Play, X } from "lucide-react";
import { toast } from "sonner";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useFramePickStore } from "@/store/frame-pick";
import {
  FrameCaptureError,
  formatFrameTime,
  frameFileName,
  lastFrameTime,
  openFrameReader,
  scaledSize,
  type FrameReader,
} from "@/utils/canvas/frame-capture";
import {
  MAX_STAGED_FRAMES,
  canStageFrame,
  ratioOfTime,
  stepFrameTime,
  thumbCount,
  thumbTimes,
  timeFromRatio,
} from "@/utils/canvas/frame-picker";

/** 节点画幅里的预览画布最长边（像素）：够清楚，又不会每帧重绘太大一块 */
const PREVIEW_MAX_EDGE = 960;
/** 胶片缩略图的最长边（像素） */
const THUMB_MAX_EDGE = 160;
/** 面板内边距（像素），算胶片轨道宽度用，要和下面的 p-3 一致 */
const PANEL_PADDING = 12;

/** 一张暂存的帧：截好的文件，加上给小块和悬浮预览用的本地地址 */
type StagedFrame = { id: number; time: number; file: File; url: string };

/** 失败原因转成给用户看的话 */
const messageOf = (error: unknown) =>
  error instanceof FrameCaptureError ? error.message : "截取失败，请重试";

type FramePickerPanelProps = {
  /** 视频地址 */
  src: string;
  /** 视频节点标题，用来给截出的图片起名 */
  label: string;
  /** 面板宽度，和生成面板一样由节点算好 */
  width: number;
  /** 点「确认」：把暂存的帧交出去建节点 */
  onConfirm: (files: File[]) => void;
  /** 退出选帧（Esc、读不出视频时的关闭、确认之后） */
  onClose: () => void;
};

/**
 * 视频节点「截取帧 → 自定义」的面板：摆在节点下方，替换生成面板。
 * 上面是胶片和播放头，拖动或方向键选时刻，节点画幅里实时显示该帧；
 * 下面是播放键和时间、已截取的帧（悬浮看大图、× 丢弃）加「截取帧」、右侧「确认」。
 * 截取只是暂存，点「确认」才一次性建出图片节点；Esc 或选中别的节点就丢弃。
 * 读视频要存储开启跨域读取；读不出时在面板里说明，不会静默失败。
 */
export function FramePickerPanel({ src, label, width, onConfirm, onClose }: FramePickerPanelProps) {
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState("");
  const [duration, setDuration] = useState(0);
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [thumbs, setThumbs] = useState<string[]>([]);
  const [staged, setStaged] = useState<StagedFrame[]>([]);
  const [busy, setBusy] = useState(false);

  const readerRef = useRef<FrameReader | null>(null);
  const trackRef = useRef<HTMLDivElement | null>(null);
  const dragging = useRef(false);
  const nextId = useRef(1);
  const stagedRef = useRef<StagedFrame[]>([]);
  const canvas = useFramePickStore((state) => state.canvas);

  useEffect(() => {
    stagedRef.current = staged;
  }, [staged]);

  // 离开时丢掉暂存帧的本地地址。
  // 不在这里让节点退出选帧：StrictMode 开发模式会先模拟卸载一次，选帧刚进入就被清掉；
  // 退出由 NodeOverlays 在它自己卸载（选中了别的节点）时负责
  useEffect(
    () => () => {
      for (const item of stagedRef.current) URL.revokeObjectURL(item.url);
    },
    [],
  );

  /** 把当前画面画进节点画幅里的预览画布 */
  const paint = useCallback(() => {
    const reader = readerRef.current;
    const target = useFramePickStore.getState().canvas;
    if (!reader || !target) return;
    const size = scaledSize(reader.size.width, reader.size.height, PREVIEW_MAX_EDGE);
    if (target.width !== size.width || target.height !== size.height) {
      target.width = size.width;
      target.height = size.height;
    }
    reader.drawTo(target);
  }, []);

  // 打开视频：预览用的 video，和下面画胶片用的 video 分开，互不抢定位
  useEffect(() => {
    let cancelled = false;
    let opened: FrameReader | null = null;
    openFrameReader(src)
      .then((reader) => {
        if (cancelled) {
          reader.dispose();
          return;
        }
        opened = reader;
        readerRef.current = reader;
        setDuration(reader.duration);
        setStatus("ready");
      })
      .catch((reason: unknown) => {
        if (cancelled) return;
        setError(messageOf(reason));
        setStatus("error");
      });
    return () => {
      cancelled = true;
      opened?.dispose();
      readerRef.current = null;
    };
  }, [src]);

  // 预览画布登记进来、或视频刚就绪：先把当前帧画上去
  useEffect(() => {
    if (status === "ready" && canvas) paint();
  }, [status, canvas, paint]);

  // 胶片缩略图：依次定位到每一格的时刻，截成小图
  const count = thumbCount(width - PANEL_PADDING * 2);
  useEffect(() => {
    if (status !== "ready" || duration <= 0) return;
    let cancelled = false;
    const urls: string[] = [];
    let strip: FrameReader | null = null;
    void (async () => {
      try {
        strip = await openFrameReader(src);
        for (const at of thumbTimes(duration, count)) {
          if (cancelled) return;
          const file = await strip.grab(at, {
            fileName: "thumb.jpg",
            maxEdge: THUMB_MAX_EDGE,
            type: "image/jpeg",
            quality: 0.7,
          });
          const url = URL.createObjectURL(file);
          if (cancelled) {
            URL.revokeObjectURL(url);
            return;
          }
          urls.push(url);
          setThumbs([...urls]);
        }
      } catch {
        // 胶片只是辅助，读不出来就留着灰格，不打扰
      } finally {
        strip?.dispose();
      }
    })();
    return () => {
      cancelled = true;
      for (const url of urls) URL.revokeObjectURL(url);
      setThumbs([]);
    };
  }, [status, duration, count, src]);

  // 播放：逐帧把画面画进节点，同时推着播放头走；播完自己停
  useEffect(() => {
    if (!playing) return;
    let raf = 0;
    const tick = () => {
      const reader = readerRef.current;
      if (!reader) return;
      setTime(reader.time());
      paint();
      if (!reader.playing()) {
        setPlaying(false);
        return;
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [playing, paint]);

  // 拖动时请求会很密：只保留最新的目标，上一次定位完了再去最新的位置
  const latest = useRef<number | null>(null);
  const seeking = useRef(false);
  const seekTo = useCallback(
    async (target: number) => {
      setTime(target);
      latest.current = target;
      if (seeking.current) return;
      seeking.current = true;
      try {
        while (latest.current !== null) {
          const next = latest.current;
          latest.current = null;
          await readerRef.current?.seek(next);
          paint();
        }
      } catch (reason) {
        toast.error(messageOf(reason));
      } finally {
        seeking.current = false;
      }
    },
    [paint],
  );

  const pause = useCallback(() => {
    readerRef.current?.pause();
    setPlaying(false);
  }, []);

  const togglePlay = useCallback(async () => {
    const reader = readerRef.current;
    if (!reader) return;
    if (playing) {
      pause();
      return;
    }
    if (reader.time() >= lastFrameTime(reader.duration) - 0.05) await seekTo(0);
    try {
      await reader.play();
      setPlaying(true);
    } catch {
      toast.error("浏览器没让视频播放，请再点一次");
    }
  }, [pause, playing, seekTo]);

  const moveByPointer = (clientX: number) => {
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || duration <= 0) return;
    void seekTo(timeFromRatio((clientX - rect.left) / rect.width, duration));
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    const arrow = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    if (arrow !== 0) {
      pause();
      void seekTo(stepFrameTime(time, arrow, event.shiftKey, duration));
    } else if (event.key === "Home") {
      pause();
      void seekTo(0);
    } else if (event.key === "End") {
      pause();
      void seekTo(timeFromRatio(1, duration));
    } else if (event.key === " ") {
      void togglePlay();
    } else {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
  };

  // Esc 退出选帧
  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onEscape);
    return () => document.removeEventListener("keydown", onEscape);
  }, [onClose]);

  const capture = async () => {
    const reader = readerRef.current;
    if (!reader || busy) return;
    pause();
    const at = reader.time();
    const check = canStageFrame(staged, at);
    if (!check.ok) {
      toast(
        check.reason === "limit"
          ? `一次最多截取 ${MAX_STAGED_FRAMES} 帧，先确认或删掉几张`
          : "这一帧已经截取过了",
      );
      return;
    }
    setBusy(true);
    try {
      const file = await reader.grab(at, { fileName: frameFileName(label, at) });
      const frame = { id: nextId.current++, time: at, file, url: URL.createObjectURL(file) };
      setStaged((list) => [...list, frame]);
    } catch (reason) {
      toast.error(messageOf(reason));
    } finally {
      setBusy(false);
    }
  };

  const remove = (id: number) =>
    setStaged((list) => {
      const gone = list.find((item) => item.id === id);
      if (gone) URL.revokeObjectURL(gone.url);
      return list.filter((item) => item.id !== id);
    });

  const ready = status === "ready";
  const ratio = ratioOfTime(time, duration);

  return (
    <div
      className="nodrag nopan nowheel bg-panel text-popover-foreground ring-foreground/5 flex flex-col gap-3 rounded-2xl p-3 text-left shadow-2xl ring-1"
      style={{ width }}
      role="group"
      aria-label="自定义截取帧"
    >
      <div
        ref={trackRef}
        role="slider"
        tabIndex={ready ? 0 : -1}
        aria-label="视频进度"
        aria-valuemin={0}
        aria-valuemax={Math.round(duration * 100) / 100}
        aria-valuenow={Math.round(time * 100) / 100}
        aria-valuetext={formatFrameTime(time)}
        aria-disabled={!ready}
        onKeyDown={ready ? onKeyDown : undefined}
        onPointerDown={(event) => {
          if (!ready) return;
          event.currentTarget.setPointerCapture(event.pointerId);
          dragging.current = true;
          pause();
          moveByPointer(event.clientX);
        }}
        onPointerMove={(event) => {
          if (dragging.current) moveByPointer(event.clientX);
        }}
        onPointerUp={(event) => {
          dragging.current = false;
          event.currentTarget.releasePointerCapture(event.pointerId);
        }}
        onPointerCancel={() => {
          dragging.current = false;
        }}
        className={cn(
          "bg-muted/50 relative flex h-11 touch-none overflow-hidden rounded-xl outline-none select-none",
          "focus-visible:ring-node-ring/60 focus-visible:ring-2",
          ready ? "cursor-ew-resize" : "cursor-default",
        )}
      >
        {Array.from({ length: count }, (_, index) => (
          <div key={index} className="bg-muted/40 min-w-0 flex-1 overflow-hidden">
            {thumbs[index] && (
              <img
                src={thumbs[index]}
                alt=""
                draggable={false}
                className="size-full object-cover"
              />
            )}
          </div>
        ))}
        {status === "loading" && (
          <div className="absolute inset-0 grid place-items-center gap-1 text-xs text-white/80">
            <span className="inline-flex items-center gap-1.5 rounded-full bg-black/55 px-2.5 py-1">
              <Loader2 className="size-3.5 animate-spin" />
              正在读取视频
            </span>
          </div>
        )}
        {status === "error" && (
          <div className="absolute inset-0 grid place-items-center bg-black/60 px-3 text-center text-xs text-white">
            {error}
          </div>
        )}
        {ready && (
          <div
            aria-hidden
            className="pointer-events-none absolute inset-y-0 w-0.5 -translate-x-1/2 rounded-full bg-white shadow-[0_0_0_1px_rgb(0_0_0/0.35)]"
            style={{ left: `${ratio * 100}%` }}
          />
        )}
      </div>

      <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-3">
        <div className="flex items-center gap-2.5">
          <button
            type="button"
            aria-label={playing ? "暂停" : "播放"}
            disabled={!ready}
            onClick={() => void togglePlay()}
            className="hover:bg-foreground/10 grid size-8 place-items-center rounded-full transition-colors disabled:opacity-40"
          >
            {playing ? (
              <Pause className="size-4 fill-current" />
            ) : (
              <Play className="size-4 fill-current" />
            )}
          </button>
          <span className="text-sm tabular-nums">
            {formatFrameTime(time)} / {formatFrameTime(duration)}
          </span>
        </div>

        <div className="flex items-center gap-1.5">
          {staged.map((frame, index) => (
            <Tooltip key={frame.id}>
              <TooltipTrigger
                delay={0}
                render={
                  <div className="ring-foreground/15 relative size-9 shrink-0 rounded-lg ring-1" />
                }
              >
                <img
                  src={frame.url}
                  alt={`第 ${index + 1} 帧`}
                  draggable={false}
                  className="size-full rounded-lg object-cover"
                />
                <span className="absolute bottom-0 left-0.5 text-[10px] leading-3 font-semibold text-sky-300 [text-shadow:0_0_3px_rgb(0_0_0/0.9)]">
                  #{index + 1}
                </span>
                <button
                  type="button"
                  aria-label={`删除第 ${index + 1} 帧`}
                  onClick={() => remove(frame.id)}
                  className="absolute -top-1 -right-1 grid size-4 place-items-center rounded-full bg-black/75 text-white hover:bg-black"
                >
                  <X className="size-2.5" />
                </button>
              </TooltipTrigger>
              <TooltipContent
                side="top"
                sideOffset={10}
                className="bg-panel text-popover-foreground ring-foreground/10 max-w-none flex-col items-start gap-1.5 rounded-xl p-2 shadow-2xl ring-1 [&>:last-child]:hidden"
              >
                <div className="flex flex-col gap-1.5">
                  <span className="px-0.5 text-xs font-semibold tabular-nums">
                    {formatFrameTime(frame.time)} 截取帧
                  </span>
                  <img
                    src={frame.url}
                    alt=""
                    draggable={false}
                    className="max-h-48 w-56 rounded-lg object-contain"
                  />
                </div>
              </TooltipContent>
            </Tooltip>
          ))}
          <button
            type="button"
            aria-label="截取帧"
            disabled={!ready || busy}
            onClick={() => void capture()}
            className={cn(
              "bg-muted/60 hover:bg-muted inline-flex h-9 items-center justify-center gap-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40",
              staged.length === 0 ? "px-3.5" : "w-9",
            )}
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Camera className="size-4" />}
            {staged.length === 0 && "截取帧"}
          </button>
        </div>

        <div className="flex justify-end">
          <button
            type="button"
            disabled={staged.length === 0 || busy}
            onClick={() => onConfirm(staged.map((item) => item.file))}
            className="bg-foreground text-background hover:bg-foreground/90 disabled:bg-muted disabled:text-muted-foreground h-9 rounded-lg px-5 text-sm font-medium transition-colors disabled:cursor-not-allowed"
          >
            确认
          </button>
        </div>
      </div>
    </div>
  );
}
