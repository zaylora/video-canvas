import { useCallback, useEffect, useId, useRef, useState, type RefObject } from "react";
import { Pause, Play, Volume2, VolumeX } from "lucide-react";
import { AnimatePresence, motion, useIsPresent, useReducedMotion } from "motion/react";
import { toast } from "sonner";

import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { createVideoSlotPool, formatMediaDuration } from "@/utils/canvas/media-lod";
import {
  LOAD_TIMEOUT_MS,
  nextFacadePhase,
  remainingLoadingMs,
  type FacadeEvent,
  type FacadePhase,
} from "@/utils/canvas/video-facade-state";

import { VideoPoster } from "./media-preview";

/** 全页面共用的 video 名额池：同时挂载的 video 受 WebMediaPlayer 数量限制 */
const videoSlots = createVideoSlotPool();

/** 秒 → m:ss；还没有有效时长时显示 0:00 */
function formatClock(seconds: number): string {
  return formatMediaDuration(seconds * 1000) ?? "0:00";
}

type VideoControlsProps = {
  videoRef: RefObject<HTMLVideoElement | null>;
};

/**
 * 自绘的精简控制条：播放/暂停、进度条、时间、静音，单行排布。
 * 原生控制条的布局和全屏、更多菜单都没法裁掉，所以不用 `controls`。
 * 整条 nodrag nowheel，拖进度条不会带动节点，视频画面区域仍可拖节点。
 */
function VideoControls({ videoRef }: VideoControlsProps) {
  const [paused, setPaused] = useState(false);
  const [muted, setMuted] = useState(false);
  const [current, setCurrent] = useState(0);
  const [total, setTotal] = useState(0);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const sync = () => {
      setPaused(video.paused);
      setMuted(video.muted);
      setCurrent(video.currentTime);
      setTotal(Number.isFinite(video.duration) ? video.duration : 0);
    };
    sync();
    const events = [
      "play",
      "pause",
      "ended",
      "timeupdate",
      "durationchange",
      "loadedmetadata",
      "volumechange",
    ];
    events.forEach((name) => video.addEventListener(name, sync));
    return () => events.forEach((name) => video.removeEventListener(name, sync));
  }, [videoRef]);

  const togglePlay = () => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused || video.ended) void video.play();
    else video.pause();
  };

  const percent = total > 0 ? (current / total) * 100 : 0;

  return (
    <div className="nodrag nowheel absolute inset-x-0 bottom-0 flex items-center gap-3.5 bg-gradient-to-t from-black/45 to-transparent px-4 pt-10 pb-3.5 text-white">
      <button
        type="button"
        aria-label={paused ? "播放" : "暂停"}
        onClick={togglePlay}
        className="grid size-6 shrink-0 place-items-center"
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
        onChange={(e) => {
          const video = videoRef.current;
          if (video) video.currentTime = Number(e.target.value);
        }}
        style={{
          background: `linear-gradient(to right, #fff ${percent}%, rgb(255 255 255 / 0.35) ${percent}%)`,
        }}
        className="h-0.5 min-w-0 flex-1 cursor-pointer appearance-none rounded-full [&::-moz-range-thumb]:size-2.5 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:bg-white [&::-webkit-slider-thumb]:size-2.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-white"
      />
      <span className="shrink-0 text-[13px] tabular-nums text-white/90">
        {formatClock(current)} / {formatClock(total)}
      </span>
      <button
        type="button"
        aria-label={muted ? "取消静音" : "静音"}
        onClick={() => {
          const video = videoRef.current;
          if (video) video.muted = !video.muted;
        }}
        className="grid size-6 shrink-0 place-items-center"
      >
        {muted ? <VolumeX className="size-[18px]" /> : <Volume2 className="size-[18px]" />}
      </button>
    </div>
  );
}

/** 进入 / 退出的补间；退出比进入快 */
const ENTER = { duration: DURATION.base, ease: EASE_OUT } as const;
const LEAVE = { duration: DURATION.exit, ease: EASE_OUT } as const;
const SWAP = { duration: DURATION.fast, ease: EASE_OUT } as const;

type FacadeVideoProps = {
  src: string;
  phase: FacadePhase;
  /** 首帧可用 */
  onFrame: () => void;
  /** 加载失败 */
  onError: () => void;
  /** 名额池要回名额 */
  onRevoke: () => void;
};

/**
 * 封面上方的 video 层：loading 时已挂载但透明，playing 时淡入；
 * 退回封面时由 AnimatePresence 留到淡出结束才卸载，所以清 src 放在卸载时，
 * 否则淡出过程中会看到黑屏。声音和名额则在开始退出的那一刻就停掉、让出。
 */
function FacadeVideo({ src, phase, onFrame, onError, onRevoke }: FacadeVideoProps) {
  const id = useId();
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const isPresent = useIsPresent();
  const onRevokeRef = useRef(onRevoke);
  useEffect(() => {
    onRevokeRef.current = onRevoke;
  });

  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    /*
     * src 在这里手动设置而不是走 JSX 属性：下面的清理会摘掉 src 来释放媒体资源，
     * StrictMode 下副作用「执行→清理→再执行」，属性写法的 src 被摘掉后 React 不会补回来，
     * 视频就永远出不了首帧、加载环转个不停
     */
    el.src = src;
    videoSlots.acquire(id, {
      // 首帧还没出来的视频也算占用中，不能被名额池顺手收走
      isPlaying: () =>
        el.readyState < HTMLMediaElement.HAVE_CURRENT_DATA || (!el.paused && !el.ended),
      release: () => onRevokeRef.current(),
    });
    return () => {
      videoSlots.release(id);
      // 只摘掉 DOM 不会释放 WebMediaPlayer，必须清掉 src 并 load()
      el.pause();
      el.removeAttribute("src");
      el.load();
    };
  }, [id, src]);

  // 开始退出：立刻停声音、让出名额
  useEffect(() => {
    if (isPresent) return;
    videoRef.current?.pause();
    videoSlots.release(id);
  }, [isPresent, id]);

  // 首帧淡入后才开播，避免用户先听到声音、后看到画面
  useEffect(() => {
    if (phase === "playing") void videoRef.current?.play().catch(() => {});
  }, [phase]);

  return (
    <motion.div
      className="absolute inset-0"
      style={{ pointerEvents: phase === "playing" && isPresent ? "auto" : "none" }}
      initial={{ opacity: 0 }}
      animate={{ opacity: phase === "playing" ? 1 : 0, transition: ENTER }}
      exit={{ opacity: 0, transition: LEAVE }}
    >
      <video
        ref={videoRef}
        playsInline
        preload="auto"
        onLoadedData={onFrame}
        onError={onError}
        className="size-full object-contain"
      />
      <VideoControls videoRef={videoRef} />
    </motion.div>
  );
}

type VideoFacadeProps = {
  /** 视频地址（不带变体） */
  src: string;
  /** 视频时长，毫秒；节点数据里没有就不传，不显示角标 */
  durationMs?: number | null;
  /** 所在节点是否被选中；失去选中会停止播放并退回封面，默认 true（不受选中状态约束） */
  active?: boolean;
};

/**
 * 视频节点的 facade：平时只显示封面、播放按钮和时长角标，不挂载 video，
 * 首屏零视频文件请求；点击后挂载 video，封面压暗、按钮变成加载环，
 * 首帧出来后 video 在封面上方淡入（设计稿 6.11）。
 * 节点失去选中、加载失败、被名额池回收时，视频停止并淡出，露出封面。
 * 填满父级盒子，画幅由父级决定。
 */
export function VideoFacade({ src, durationMs, active = true }: VideoFacadeProps) {
  const reduce = useReducedMotion();
  const [phase, setPhase] = useState<FacadePhase>("idle");
  const [frameReady, setFrameReady] = useState(false);
  /** 封面是否已就绪（加载完或确定没有封面）；播放钮和时长角标等它一起淡入 */
  const [posterSettled, setPosterSettled] = useState(false);
  /** 每次点播放换一个，旧 video 还在淡出时不会被新一轮播放复用 */
  const [session, setSession] = useState(0);
  const startedAt = useRef(0);
  const duration = formatMediaDuration(durationMs);

  const dispatch = useCallback(
    (event: FacadeEvent) => setPhase((current) => nextFacadePhase(current, event)),
    [],
  );
  const fail = useCallback(() => {
    dispatch("error");
    toast.error("视频无法播放");
  }, [dispatch]);

  // 节点失去选中：停止播放，退回封面。
  // 只在 active 发生变化时触发：点击未选中节点上的播放按钮时，按钮先于节点选中收到点击，
  // 此刻 active 仍是 false，不能把刚开始的加载当场退回。
  const [prevActive, setPrevActive] = useState(active);
  if (prevActive !== active) {
    setPrevActive(active);
    if (!active) setPhase(nextFacadePhase(phase, "deactivate"));
  }

  // 首帧到了，但加载态要满最短展示时长才淡入 video，免得加载环一闪而过
  useEffect(() => {
    if (phase !== "loading" || !frameReady) return;
    const timer = setTimeout(
      () => dispatch("ready"),
      remainingLoadingMs(startedAt.current, performance.now()),
    );
    return () => clearTimeout(timer);
  }, [phase, frameReady, dispatch]);

  // 迟迟没有首帧就按失败处理，别让加载环永远转下去
  useEffect(() => {
    if (phase !== "loading") return;
    const timer = setTimeout(fail, LOAD_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [phase, fail]);

  const loading = phase === "loading";
  const playing = phase === "playing";
  const transition = playing ? LEAVE : ENTER;

  return (
    <div className="relative size-full">
      <VideoPoster src={src} className="object-contain" onSettledChange={setPosterSettled} />
      <AnimatePresence>
        {phase !== "idle" && (
          <FacadeVideo
            key={session}
            src={src}
            phase={phase}
            onFrame={() => setFrameReady(true)}
            onError={fail}
            onRevoke={() => dispatch("revoke")}
          />
        )}
      </AnimatePresence>
      {/* 加载中把封面压暗，视觉上和 video 淡入前后衔接 */}
      <motion.div
        className="pointer-events-none absolute inset-0 bg-black/30"
        initial={false}
        animate={{ opacity: loading ? 1 : 0 }}
        transition={loading ? ENTER : LEAVE}
      />
      <motion.button
        type="button"
        aria-label="播放视频"
        disabled={phase !== "idle"}
        initial={false}
        animate={{
          opacity: playing || !posterSettled ? 0 : 1,
          scale: playing && !reduce ? 0.9 : 1,
        }}
        whileHover={phase === "idle" && !reduce ? { scale: 1.06 } : undefined}
        whileTap={phase === "idle" ? TAP : undefined}
        transition={transition}
        onClick={() => {
          startedAt.current = performance.now();
          setFrameReady(false);
          setSession((n) => n + 1);
          dispatch("play");
        }}
        className="nodrag absolute inset-0 m-auto grid size-12 place-items-center rounded-full text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white"
        style={{ pointerEvents: playing ? "none" : undefined }}
      >
        {/* 按钮底：加载中淡掉，只剩压暗的封面和环 */}
        <motion.span
          className="absolute inset-0 rounded-full bg-black/55 shadow-lg backdrop-blur"
          initial={false}
          animate={{ opacity: loading ? 0 : 1 }}
          transition={SWAP}
        />
        <motion.span
          className="relative col-start-1 row-start-1"
          initial={false}
          animate={{ opacity: loading ? 0 : 1, scale: loading && !reduce ? 0.6 : 1 }}
          transition={SWAP}
        >
          <Play className="size-5 translate-x-px fill-current" />
        </motion.span>
        <motion.span
          className="relative col-start-1 row-start-1"
          initial={false}
          animate={{ opacity: loading ? 1 : 0, scale: loading || reduce ? 1 : 0.8 }}
          transition={SWAP}
        >
          {/* 减少动态效果时环不旋转，只留静止的弧 */}
          <svg
            viewBox="0 0 28 28"
            className={reduce ? "size-7" : "size-7 animate-spin [animation-duration:0.9s]"}
            aria-hidden
          >
            <circle
              cx="14"
              cy="14"
              r="11"
              fill="none"
              stroke="currentColor"
              strokeOpacity="0.25"
              strokeWidth="2.4"
            />
            <circle
              cx="14"
              cy="14"
              r="11"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.4"
              strokeLinecap="round"
              strokeDasharray="22 66"
            />
          </svg>
        </motion.span>
      </motion.button>
      <span role="status" className="sr-only">
        {loading ? "视频加载中" : ""}
      </span>
      {duration && (
        <motion.span
          className="pointer-events-none absolute right-2 bottom-2 rounded-md bg-black/60 px-1.5 py-0.5 text-[11px] leading-4 text-white tabular-nums"
          initial={false}
          animate={{ opacity: playing || !posterSettled ? 0 : 1 }}
          transition={transition}
        >
          {duration}
        </motion.span>
      )}
    </div>
  );
}
