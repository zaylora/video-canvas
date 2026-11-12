import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CircleX,
  Columns2,
  Download,
  Film,
  Loader2,
  Maximize2,
  Pause,
  Play,
  RotateCcw,
  SkipBack,
  SkipForward,
  Square,
  Volume2,
  VolumeX,
} from "lucide-react";

import type { Project, Script } from "@/api/marketing/type";
import { ChromeButton, ChromePill } from "@/components/canvas/chrome/chrome";
import { cn } from "@/lib/utils";
import { useWorkbenchStore, type WorkbenchControls } from "@/store/marketing-workbench";
import { useMarketingOptions } from "@/hooks/use-marketing-options";
import { playableTakes, roundAspect, scriptRenderState } from "@/utils/marketing/script";
import { compareState, scriptDuration, shotIndexAt } from "@/utils/marketing/workbench";

import { downloadUrl, takeFileName } from "./script-dialogs";
import { useRenderProgress, useWorkbenchTarget } from "./hooks";
import type { MarketingActions } from "./use-actions";

/** 时间轴上的一段：起止、标题、摘要；pair 是跟拍版对应的参考镜头号 */
interface TimelineShot {
  /** 参考镜头号（原片镜头用）；成片镜头为下标 + 1 */
  id: number;
  start: number;
  end: number;
  /** 画面描述（原片为主体 + 动作） */
  text: string;
  /** 旁白（原片为台词） */
  voice: string;
  pair: number | null;
}

/** 秒 → 00:04.2 */
function clock(seconds: number): string {
  const safe = Math.max(0, seconds);
  const m = Math.floor(safe / 60);
  const s = safe - m * 60;
  return `${String(m).padStart(2, "0")}:${s.toFixed(1).padStart(4, "0")}`;
}

/** 刻度间隔：让整条刻度尺大约 8–16 格 */
function tickStep(total: number): number {
  const steps = [0.5, 1, 2, 5, 10, 15, 30, 60];
  return steps.find((step) => total / step <= 16) ?? 60;
}

/**
 * 剪辑软件式的时间轴（只读，只用来看和定位）：顶部秒刻度尺，下面一条连续的灰色轨道（播放过的部分稍亮），
 * 一根播放头竖线贯穿刻度尺和轨道。在刻度尺或轨道上按下即把播放头移到那里，按住拖动可来回看；不能剪。
 * 只有它订阅每帧的时间，其余部分不随播放重绘。
 */
function Timeline({
  total,
  onSeek,
}: {
  total: number;
  /** 移动播放头到时间 t（秒）；拖动过程中不自动播放 */
  onSeek: (t: number) => void;
}) {
  const time = useWorkbenchStore((state) => state.time);
  const areaRef = useRef<HTMLDivElement>(null);
  const dragging = useRef(false);
  const step = tickStep(total);
  const ticks =
    total > 0 ? Array.from({ length: Math.floor(total / step) + 1 }, (_, i) => i * step) : [];
  const pct = (t: number) => `${total > 0 ? Math.min(Math.max(t / total, 0), 1) * 100 : 0}%`;

  const timeAt = (clientX: number) => {
    const rect = areaRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) return 0;
    return Math.min(Math.max((clientX - rect.left) / rect.width, 0), 0.999) * total;
  };

  return (
    <div
      data-slot="marketing-timeline"
      className="bg-background/40 ring-chrome-border grid min-w-0 rounded-[20px] p-2.5 ring-1 select-none"
    >
      <div
        ref={areaRef}
        role="slider"
        tabIndex={-1}
        aria-label="时间轴"
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={time}
        onPointerDown={(event) => {
          dragging.current = true;
          event.currentTarget.setPointerCapture?.(event.pointerId);
          onSeek(timeAt(event.clientX));
        }}
        onPointerMove={(event) => dragging.current && onSeek(timeAt(event.clientX))}
        onPointerUp={(event) => {
          dragging.current = false;
          event.currentTarget.releasePointerCapture?.(event.pointerId);
        }}
        className="relative grid min-w-0 cursor-pointer grid-rows-[1.25rem] auto-rows-[1.75rem] gap-y-1"
      >
        <div className="border-chrome-border relative border-b">
          {ticks.map((tick) => (
            <span
              key={tick}
              className="text-muted-foreground absolute top-0 -translate-x-1/2 font-mono text-[10px] tabular-nums first:translate-x-0"
              style={{ left: pct(tick) }}
            >
              {Number.isInteger(tick) ? tick : tick.toFixed(1)}s
              <i className="bg-chrome-border absolute top-full left-1/2 h-1 w-px" />
            </span>
          ))}
        </div>
        <div className="bg-muted-foreground/20 relative min-w-0 overflow-hidden rounded-lg">
          <i
            className="bg-muted-foreground/35 absolute inset-y-0 left-0"
            style={{ width: pct(time) }}
          />
        </div>
        <i
          aria-hidden
          className="bg-foreground pointer-events-none absolute -top-0.5 bottom-0 w-px"
          style={{ left: pct(time) }}
        >
          <span className="bg-foreground absolute -top-0.5 left-1/2 size-2.5 -translate-x-1/2 rotate-45 rounded-[2px]" />
        </i>
      </div>
    </div>
  );
}

/** 监视器上只留：暂停时中间的大播放键、缓冲时的转圈；点画面播放 / 暂停 */
function PlayerOverlay({
  videoRef,
  src,
}: {
  videoRef: React.RefObject<HTMLVideoElement | null>;
  /** 视频换了要重新监听 */
  src: string;
}) {
  const playing = useWorkbenchStore((state) => state.playing);
  const [buffering, setBuffering] = useState(false);
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const onWaiting = () => setBuffering(true);
    const onReady = () => setBuffering(false);
    const events: [string, () => void][] = [
      ["waiting", onWaiting],
      ["playing", onReady],
      ["canplay", onReady],
      ["pause", onReady],
    ];
    events.forEach(([name, fn]) => video.addEventListener(name, fn));
    return () => events.forEach(([name, fn]) => video.removeEventListener(name, fn));
  }, [videoRef, src]);

  return (
    <div
      data-slot="marketing-player-overlay"
      onClick={() => useWorkbenchStore.getState().controls?.toggle()}
      className="absolute inset-0 cursor-pointer rounded-[22px]"
    >
      {(buffering || !playing) && (
        <span
          aria-hidden
          className="absolute inset-0 m-auto grid size-14 place-items-center rounded-full bg-black/45 text-white backdrop-blur-sm transition-transform duration-150 group-hover/player:scale-105"
        >
          {buffering ? (
            <Loader2 className="size-6 animate-spin" />
          ) : (
            <Play className="ml-0.5 size-6 fill-current" />
          )}
        </span>
      )}
    </div>
  );
}

/** 走带条：上一镜、播放 / 暂停、下一镜、时间码、第几镜、静音、全屏 */
function TransportBar({
  videoRef,
  src,
  shots,
  total,
  showMute,
}: {
  videoRef: React.RefObject<HTMLVideoElement | null>;
  src: string;
  shots: { start: number; end: number }[];
  total: number;
  showMute: boolean;
}) {
  const time = useWorkbenchStore((state) => state.time);
  const playing = useWorkbenchStore((state) => state.playing);
  const [muted, setMuted] = useState(false);
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const sync = () => setMuted(video.muted);
    video.addEventListener("volumechange", sync);
    sync();
    return () => video.removeEventListener("volumechange", sync);
  }, [videoRef, src]);

  const index = shotIndexAt(shots, time);
  const jump = (offset: 1 | -1) => {
    const store = useWorkbenchStore.getState();
    if (shots.length === 0) return;
    const next = Math.min(Math.max(index + offset, 0), shots.length - 1);
    store.controls?.seek(shots[next].start, store.playing);
  };

  return (
    <div className="flex min-w-0 items-center gap-1">
      <ChromeButton
        aria-label="上一镜（←）"
        title="上一镜（←）"
        disabled={index <= 0}
        onClick={() => jump(-1)}
      >
        <SkipBack />
      </ChromeButton>
      <ChromeButton
        aria-label={playing ? "暂停（空格）" : "播放（空格）"}
        title={playing ? "暂停（空格）" : "播放（空格）"}
        onClick={() => useWorkbenchStore.getState().controls?.toggle()}
        className="bg-chrome-hover ring-chrome-border ring-1"
      >
        {playing ? <Pause className="fill-current" /> : <Play className="fill-current" />}
      </ChromeButton>
      <ChromeButton
        aria-label="下一镜（→）"
        title="下一镜（→）"
        disabled={shots.length === 0 || index >= shots.length - 1}
        onClick={() => jump(1)}
      >
        <SkipForward />
      </ChromeButton>
      <span className="ml-2 font-mono text-xs whitespace-nowrap tabular-nums">
        {clock(time)}
        <span className="text-muted-foreground"> / {clock(total)}</span>
      </span>
      {shots.length > 0 && (
        <span className="text-muted-foreground ml-2 text-xs whitespace-nowrap tabular-nums">
          镜 {Math.max(index, 0) + 1} / {shots.length}
        </span>
      )}
      <span className="flex-1" />
      {showMute && (
        <ChromeButton
          aria-label={muted ? "取消静音" : "静音"}
          title={muted ? "取消静音" : "静音"}
          onClick={() => {
            const video = videoRef.current;
            if (video) video.muted = !video.muted;
          }}
        >
          {muted ? <VolumeX /> : <Volume2 />}
        </ChromeButton>
      )}
      <ChromeButton
        aria-label="全屏（F）"
        title="全屏（F）"
        onClick={() => useWorkbenchStore.getState().controls?.fullscreen()}
      >
        <Maximize2 />
      </ChromeButton>
    </div>
  );
}

/** 舞台中央的状态卡：未出片、出片中、重写中、失败、轮次失败、写脚本中 */
function StateCard({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid max-w-sm justify-items-center gap-3 px-6 text-center text-[13.5px]">
      {children}
    </div>
  );
}

/** 视频加载失败：原位提示 + 重试 */
function VideoError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="absolute inset-0 grid place-content-center justify-items-center gap-2 bg-black/70 text-[13px] text-white">
      <CircleX className="size-5" />
      视频加载失败
      <button
        type="button"
        onClick={onRetry}
        className="rounded-full bg-white/15 px-3 py-1 text-xs hover:bg-white/25"
      >
        重试
      </button>
    </div>
  );
}

/** 未出片脚本在舞台上的样子 */
function ScriptState({
  script,
  aspect,
  available,
  actions,
}: {
  script: Script;
  /** 成片画幅（宽、高）：出片中的画框按它画 */
  aspect: [number, number];
  available: number | null;
  actions: MarketingActions;
}) {
  const progress = useRenderProgress(script);
  const render = scriptRenderState(script, available);
  const rendering = actions.busy[`render:${script.id}`];
  const renderButton = (label: string, icon: React.ReactNode) => (
    <button
      type="button"
      disabled={render.disabled || rendering}
      title={render.reason || undefined}
      onClick={() => void actions.render(script)}
      className="bg-foreground text-background focus-visible:ring-ring/50 inline-flex h-10 items-center gap-2 rounded-full px-5 text-sm font-medium outline-none transition-opacity hover:opacity-90 focus-visible:ring-3 disabled:opacity-40"
    >
      {rendering ? <Loader2 className="size-4 animate-spin" /> : icon}
      {label}
      <span className="tabular-nums opacity-65">✦{script.renderCredits}</span>
    </button>
  );

  if (script.status === "rendering") {
    const pct = Math.min(100, Math.max(0, Math.round(progress ?? 0)));
    const circumference = 2 * Math.PI * 26;
    /** 还没拿到进度：一段弧转圈；拿到后按百分比平滑填满 */
    const waiting = pct === 0;
    return (
      <div
        data-slot="marketing-rendering"
        className="bg-background/40 ring-chrome-border relative grid h-full max-h-full max-w-full place-items-center overflow-hidden rounded-[22px] ring-1"
        style={{ aspectRatio: `${aspect[0]} / ${aspect[1]}` }}
      >
        <div aria-hidden className="marketing-shimmer absolute inset-0" />
        <div className="relative grid justify-items-center gap-3">
          <svg
            viewBox="0 0 60 60"
            className={cn(
              "size-16 -rotate-90",
              waiting && "animate-spin motion-reduce:animate-none",
            )}
            aria-hidden
          >
            <circle
              cx="30"
              cy="30"
              r="26"
              fill="none"
              stroke="currentColor"
              strokeOpacity=".15"
              strokeWidth="4"
            />
            <circle
              cx="30"
              cy="30"
              r="26"
              fill="none"
              stroke="var(--beam)"
              strokeWidth="4"
              strokeLinecap="round"
              strokeDasharray={circumference}
              strokeDashoffset={circumference * (waiting ? 0.75 : 1 - pct / 100)}
              className="transition-[stroke-dashoffset] duration-500 ease-out"
            />
          </svg>
          <b className="font-mono text-[13.5px] tabular-nums">出片中 {pct}%</b>
        </div>
      </div>
    );
  }
  if (script.status === "rewriting") {
    return (
      <StateCard>
        <Loader2 className="size-7 animate-spin" />
        <b>重写中</b>
      </StateCard>
    );
  }
  if (script.status === "failed") {
    return (
      <StateCard>
        <CircleX className="text-destructive size-7" />
        <b>出片失败</b>
        <span className="text-muted-foreground">{script.error || "原因未知"}</span>
        {renderButton("重试", <RotateCcw className="size-4" />)}
      </StateCard>
    );
  }
  return (
    <StateCard>
      <Film className="text-muted-foreground size-7" />
      <b className="text-[15px]">{script.title}</b>
      {renderButton("出片", <Play className="size-4" />)}
      {render.disabled && render.reason && (
        <span className="text-destructive text-[12.5px]">{render.reason}</span>
      )}
    </StateCard>
  );
}

/**
 * 舞台（创作台中间栏）：选中项的监视器 + 走带条 + 只读时间轴；脚本已出片且本轮有参考片时可切「对比原片」，
 * 两个视频并排各自正常播放（一起播放 / 暂停，定位时原片跳到同一时间点），声音二选一。
 * 选中项不能播放时显示状态卡（待出片 / 出片中 / 重写中 / 失败；轮次失败、写脚本中）。
 * 向 store 注册播放控制，检查器、时间轴和快捷键都通过它跳转。
 */
export function Stage({
  project,
  available,
  actions,
}: {
  project: Project;
  available: number | null;
  actions: MarketingActions;
}) {
  const { round, selection, script } = useWorkbenchTarget(project.id);
  const mode = useWorkbenchStore((state) => state.mode);
  const audio = useWorkbenchStore((state) => state.audio);
  const takeIndex = useWorkbenchStore((state) => state.take);
  const { angles } = useMarketingOptions();

  const mainRef = useRef<HTMLVideoElement>(null);
  const refRef = useRef<HTMLVideoElement>(null);
  const viewRef = useRef<HTMLDivElement>(null);
  const [errored, setErrored] = useState<Record<string, boolean>>({});
  const [reloadKey, setReloadKey] = useState(0);

  const takes = script ? playableTakes(script) : [];
  const take =
    takes.length > 0 ? takes[Math.min(takeIndex ?? takes.length - 1, takes.length - 1)] : null;
  const isRef = selection === "ref";
  const mainSrc = isRef
    ? (round?.refVideo?.url ?? "")
    : script?.status === "done"
      ? (take?.outputs[0]?.url ?? "")
      : "";
  const poster = isRef ? undefined : take?.outputs[0]?.posterUrl || undefined;
  const compare = round ? compareState(round, script) : { ok: false as const, reason: "" };
  const comparing = mode === "compare" && compare.ok && !!round?.refVideo;
  const [w, h] = round ? roundAspect(round.settings) : [9, 16];

  const mainShots = useMemo<TimelineShot[]>(() => {
    if (isRef) {
      return (round?.reverse?.shots ?? []).map((shot) => ({
        id: shot.id,
        start: shot.start,
        end: shot.end,
        text: shot.action,
        voice: shot.dialogue,
        pair: null,
      }));
    }
    return (script?.shots ?? []).map((shot, index) => ({
      id: index + 1,
      start: shot.start,
      end: shot.end,
      text: shot.scene,
      voice: shot.narration,
      pair: script?.kind === "follow" ? shot.refShot : null,
    }));
  }, [isRef, round?.reverse, script]);
  const mainTotal = isRef
    ? (round?.refVideo?.duration ?? mainShots.at(-1)?.end ?? 0)
    : script
      ? scriptDuration(script)
      : 0;

  /** 把 video 的时间与播放状态写进 store；播放时逐帧更新，让播放头顺滑 */
  useEffect(() => {
    const video = mainRef.current;
    if (!video || !mainSrc) return;
    const { setClock } = useWorkbenchStore.getState();
    let frame = 0;
    const sync = () => setClock(video.currentTime, !video.paused && !video.ended);
    const loop = () => {
      sync();
      if (!video.paused) frame = requestAnimationFrame(loop);
    };
    const onPlay = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(loop);
    };
    const onMeta = () => {
      const pending = useWorkbenchStore.getState().pendingSeek;
      if (pending !== null) {
        video.currentTime = pending;
        useWorkbenchStore.getState().setPendingSeek(null);
        void video.play().catch(() => {});
      }
      sync();
    };
    const events: [string, () => void][] = [
      ["play", onPlay],
      ["pause", sync],
      ["ended", sync],
      ["seeked", sync],
      ["timeupdate", sync],
      ["loadedmetadata", onMeta],
    ];
    events.forEach(([name, fn]) => video.addEventListener(name, fn));
    if (video.readyState >= 1) onMeta();
    else setClock(0, false);
    return () => {
      cancelAnimationFrame(frame);
      events.forEach(([name, fn]) => video.removeEventListener(name, fn));
    };
  }, [mainSrc, reloadKey, comparing]);

  /** 选中项没变、视频已就绪时又来了一次待跳转（比如在原片上点「参考 n」） */
  const pendingSeek = useWorkbenchStore((state) => state.pendingSeek);
  useEffect(() => {
    const video = mainRef.current;
    if (pendingSeek === null || !video || video.readyState < 1) return;
    video.currentTime = pendingSeek;
    useWorkbenchStore.getState().setPendingSeek(null);
    void video.play().catch(() => {});
  }, [pendingSeek]);

  /** 注册播放控制 */
  const seek = useCallback((t: number, play = true) => {
    const video = mainRef.current;
    if (!video) return;
    if (video.readyState < 1) {
      useWorkbenchStore.getState().setPendingSeek(t);
      return;
    }
    video.currentTime = Math.max(0, t);
    if (play) void video.play().catch(() => {});
    useWorkbenchStore.getState().setClock(video.currentTime, !video.paused);
  }, []);
  useEffect(() => {
    if (!mainSrc) {
      useWorkbenchStore.getState().setControls(null);
      return;
    }
    const controls: WorkbenchControls = {
      seek,
      toggle: () => {
        const video = mainRef.current;
        if (!video) return;
        if (video.paused) void video.play().catch(() => {});
        else video.pause();
      },
      pause: () => mainRef.current?.pause(),
      fullscreen: () => {
        const view = viewRef.current;
        if (!view) return;
        if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
        else void view.requestFullscreen?.().catch(() => {});
      },
    };
    useWorkbenchStore.getState().setControls(controls);
    return () => useWorkbenchStore.getState().setControls(null);
  }, [mainSrc, seek]);

  /**
   * 对比：两个视频各自正常速度播放，不强行对齐（强行对齐会频繁跳转、一卡一卡）。
   * 只联动三件事：一起播放、一起暂停；在成片上定位（拖时间轴、点镜头）时原片跳到同一时间点（超出原片时长停在结尾）。
   */
  useEffect(() => {
    const main = mainRef.current;
    const ref = refRef.current;
    if (!comparing || !main || !ref) return;
    const onPlay = () => void ref.play().catch(() => {});
    const onPause = () => ref.pause();
    const onSeeked = () => {
      const end = Number.isFinite(ref.duration) ? ref.duration : main.currentTime;
      ref.currentTime = Math.min(main.currentTime, Math.max(end - 0.05, 0));
    };
    main.addEventListener("play", onPlay);
    main.addEventListener("pause", onPause);
    main.addEventListener("seeked", onSeeked);
    onSeeked();
    if (!main.paused) onPlay();
    return () => {
      main.removeEventListener("play", onPlay);
      main.removeEventListener("pause", onPause);
      main.removeEventListener("seeked", onSeeked);
    };
  }, [comparing, mainSrc]);

  useEffect(() => {
    if (mainRef.current) mainRef.current.muted = comparing && audio === "ref";
    if (refRef.current) refRef.current.muted = !comparing || audio !== "ref";
  }, [comparing, audio, mainSrc]);

  if (!round) return <section className={STAGE_CLASS} />;

  // —— 轮次级状态 ——
  let view: React.ReactNode = null;
  if (round.status === "failed" || round.status === "canceled") {
    const failedStep = round.steps.find((step) => step.status === "failed");
    view = (
      <StateCard>
        <CircleX
          className={cn(
            "size-7",
            round.status === "failed" ? "text-destructive" : "text-muted-foreground",
          )}
        />
        <b>{round.status === "failed" ? round.error || "写脚本失败" : "已取消"}</b>
        <span className="text-muted-foreground">积分已退回</span>
        {round.status === "failed" && failedStep && (
          <button
            type="button"
            disabled={actions.busy[`step:${failedStep.id}`]}
            onClick={() => void actions.retryStep(failedStep.id)}
            className="bg-foreground text-background inline-flex h-10 items-center gap-2 rounded-full px-5 text-sm font-medium disabled:opacity-40"
          >
            <RotateCcw className="size-4" />
            重试
          </button>
        )}
      </StateCard>
    );
  } else if (!mainSrc) {
    if (script)
      view = (
        <ScriptState script={script} aspect={[w, h]} available={available} actions={actions} />
      );
    else if (round.status === "scripting") {
      view = (
        <StateCard>
          <Loader2 className="text-status-running size-7 animate-spin" />
          <b>{round.stage || "写脚本中"}</b>
        </StateCard>
      );
    }
  }

  const playable = !!mainSrc && !view;
  const fileName = script
    ? takeFileName(project, script, takes.indexOf(take!), angles)
    : `${project.title}-参考原片.mp4`;
  const error = errored[`${mainSrc}#${reloadKey}`];
  const retry = () => {
    setErrored({});
    setReloadKey((key) => key + 1);
  };

  return (
    <section data-slot="marketing-stage" className={STAGE_CLASS}>
      {playable && (
        <div className="flex flex-wrap items-center gap-2 px-3 pt-3">
          <ChromePill className="h-9 shadow-none">
            <ChromeButton
              active={!comparing}
              onClick={() => useWorkbenchStore.getState().setMode("single")}
              className={cn(!comparing && "bg-chrome-hover ring-chrome-border ring-1")}
            >
              <Square />
              单看
            </ChromeButton>
            <ChromeButton
              active={comparing}
              disabled={!compare.ok}
              title={compare.ok ? "对比原片（C）" : compare.reason}
              onClick={() => useWorkbenchStore.getState().setMode("compare")}
              className={cn(comparing && "bg-chrome-hover ring-chrome-border ring-1")}
            >
              <Columns2 />
              对比原片
            </ChromeButton>
          </ChromePill>
          {!isRef && takes.length > 1 && (
            <ChromePill className="h-9 shadow-none" aria-label="第几条成片">
              {takes.map((item, index) => (
                <ChromeButton
                  key={item.id}
                  active={item === take}
                  onClick={() => useWorkbenchStore.getState().setTake(index)}
                  className={cn(item === take && "bg-chrome-hover ring-chrome-border ring-1")}
                >
                  第 {index + 1} 条
                </ChromeButton>
              ))}
            </ChromePill>
          )}
          {comparing && (
            <ChromePill className="h-9 shadow-none" aria-label="对比时听哪边">
              <ChromeButton
                active={audio === "ref"}
                onClick={() => useWorkbenchStore.getState().setAudio("ref")}
                className={cn(audio === "ref" && "bg-chrome-hover ring-chrome-border ring-1")}
              >
                <Volume2 />
                原片声
              </ChromeButton>
              <ChromeButton
                active={audio !== "ref"}
                onClick={() => useWorkbenchStore.getState().setAudio("main")}
                className={cn(audio !== "ref" && "bg-chrome-hover ring-chrome-border ring-1")}
              >
                成片声
              </ChromeButton>
            </ChromePill>
          )}
          <div className="flex-1" />
          <ChromePill className="h-9 shadow-none">
            <ChromeButton
              aria-label="下载"
              title="下载"
              onClick={() => downloadUrl(mainSrc, fileName)}
            >
              <Download />
            </ChromeButton>
          </ChromePill>
        </div>
      )}

      <div
        ref={viewRef}
        className="flex min-h-0 flex-1 items-center justify-center gap-5 p-4 [&:fullscreen]:bg-black"
      >
        {view}
        {playable && comparing && round.refVideo && (
          <figure className="flex h-full min-w-0 flex-1 flex-col items-center gap-1.5">
            <video
              key={`ref-${round.refVideo.url}`}
              ref={refRef}
              src={round.refVideo.url}
              playsInline
              muted
              preload="auto"
              className="min-h-0 flex-1 rounded-[22px] bg-black object-contain shadow-[0_14px_40px_-18px_oklch(0_0_0/0.7)]"
              style={{ aspectRatio: `${w} / ${h}` }}
            />
            <figcaption className="text-muted-foreground text-xs">原片</figcaption>
          </figure>
        )}
        {playable && (
          <figure
            className={cn(
              "relative flex h-full min-w-0 flex-col items-center gap-1.5",
              comparing && "flex-1",
            )}
          >
            <div
              className="group/player relative min-h-0 flex-1"
              style={{ aspectRatio: `${w} / ${h}` }}
            >
              <video
                key={`${mainSrc}#${reloadKey}`}
                ref={mainRef}
                src={mainSrc}
                poster={poster}
                playsInline
                preload="metadata"
                onError={() =>
                  setErrored((prev) => ({ ...prev, [`${mainSrc}#${reloadKey}`]: true }))
                }
                className="size-full cursor-pointer rounded-[22px] bg-black object-contain shadow-[0_14px_40px_-18px_oklch(0_0_0/0.7)]"
              />
              {error ? (
                <VideoError onRetry={retry} />
              ) : (
                <PlayerOverlay videoRef={mainRef} src={`${mainSrc}#${reloadKey}`} />
              )}
            </div>
            {comparing && <figcaption className="text-muted-foreground text-xs">成片</figcaption>}
          </figure>
        )}
      </div>

      {playable && (
        <StageFooter
          videoRef={mainRef}
          src={`${mainSrc}#${reloadKey}`}
          mainShots={mainShots}
          mainTotal={mainTotal}
          comparing={comparing}
          onSeek={seek}
        />
      )}
    </section>
  );
}

const STAGE_CLASS =
  "bg-chrome ring-chrome-border flex min-h-0 flex-col overflow-hidden rounded-[28px] ring-1 backdrop-blur-xl max-lg:min-h-[70svh]";

/** 监视器下方：走带条 + 只读时间轴（一条连续的灰色轨道，播放过的部分稍亮） */
function StageFooter({
  videoRef,
  src,
  mainShots,
  mainTotal,
  comparing,
  onSeek,
}: {
  videoRef: React.RefObject<HTMLVideoElement | null>;
  src: string;
  mainShots: TimelineShot[];
  mainTotal: number;
  comparing: boolean;
  onSeek: (t: number, play?: boolean) => void;
}) {
  return (
    <div className="grid min-w-0 shrink-0 gap-2 px-4 pt-1 pb-4">
      <TransportBar
        videoRef={videoRef}
        src={src}
        shots={mainShots}
        total={mainTotal}
        showMute={!comparing}
      />
      <Timeline total={mainTotal} onSeek={(t) => onSeek(t, useWorkbenchStore.getState().playing)} />
    </div>
  );
}
