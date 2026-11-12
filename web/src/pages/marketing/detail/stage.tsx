import { useCallback, useEffect, useRef, useState } from "react";
import { CircleX, Download, Film, Loader2, Play, RotateCcw } from "lucide-react";

import type { Project, Script } from "@/api/marketing/type";
import { ChromeButton, ChromePill } from "@/components/canvas/chrome/chrome";
import { VideoPlayer } from "@/components/video-player";
import { cn } from "@/lib/utils";
import { useWorkbenchStore, type WorkbenchControls } from "@/store/marketing-workbench";
import { useMarketingOptions } from "@/hooks/use-marketing-options";
import { playableTakes, roundAspect, scriptRenderState } from "@/utils/marketing/script";
import { scriptDuration } from "@/utils/marketing/workbench";

import { downloadUrl, takeFileName } from "./script-dialogs";
import { useRenderProgress, useWorkbenchTarget } from "./hooks";
import type { MarketingActions } from "./use-actions";

/** 舞台中央的状态卡：未出片、出片中、重写中、失败、轮次失败、写脚本中 */
function StateCard({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid max-w-sm justify-items-center gap-3 px-6 text-center text-[13.5px]">
      {children}
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
  const takeIndex = useWorkbenchStore((state) => state.take);
  const { angles } = useMarketingOptions();

  const viewRef = useRef<HTMLDivElement>(null);
  /**
   * 共享播放器里正在播放的 video 元素（停在封面时为 null）。元素放在 ref 里（要直接设 currentTime、play），
   * 挂上 / 卸下时让计数器加一，依赖它的副作用随之重新执行。
   */
  const mainElRef = useRef<HTMLVideoElement | null>(null);
  const [mainTick, setMainTick] = useState(0);
  const onMainVideo = useCallback((el: HTMLVideoElement | null) => {
    mainElRef.current = el;
    setMainTick((n) => n + 1);
  }, []);
  /** 让停在封面的播放器开始播放：数值一变就开始（见 VideoPlayer 的 playSignal） */
  const [mainSignal, setMainSignal] = useState(0);

  const takes = script ? playableTakes(script) : [];
  const take =
    takes.length > 0 ? takes[Math.min(takeIndex ?? takes.length - 1, takes.length - 1)] : null;
  const isRef = selection === "ref";
  const mainSrc = isRef
    ? (round?.refVideo?.url ?? "")
    : script?.status === "done"
      ? (take?.outputs[0]?.url ?? "")
      : "";
  const [w, h] = round ? roundAspect(round.settings) : [9, 16];

  const mainTotal = isRef
    ? (round?.refVideo?.duration ?? round?.reverse?.shots.at(-1)?.end ?? 0)
    : script
      ? scriptDuration(script)
      : 0;

  /** 把 video 的时间与播放状态写进 store；播放时逐帧更新，让播放头顺滑。video 挂上时先执行待跳转 */
  useEffect(() => {
    const { setClock } = useWorkbenchStore.getState();
    const mainEl = mainElRef.current;
    if (!mainEl) {
      setClock(useWorkbenchStore.getState().time, false);
      return;
    }
    const video = mainEl;
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
    if (!video.paused) onPlay();
    return () => {
      cancelAnimationFrame(frame);
      events.forEach(([name, fn]) => video.removeEventListener(name, fn));
    };
  }, [mainTick]);

  /** 视频已就绪时又来了一次待跳转（比如在原片上又点了一次「参考 n」） */
  const pendingSeek = useWorkbenchStore((state) => state.pendingSeek);
  useEffect(() => {
    const mainEl = mainElRef.current;
    if (pendingSeek === null || !mainEl || mainEl.readyState < 1) return;
    mainEl.currentTime = pendingSeek;
    useWorkbenchStore.getState().setPendingSeek(null);
    void mainEl.play().catch(() => {});
  }, [pendingSeek, mainTick]);

  /** 跳到时间 t：播放器还停在封面时先记下位置并让它开始播放，video 挂上后再跳 */
  const seek = useCallback((t: number, play = true) => {
    const video = mainElRef.current;
    if (!video) {
      useWorkbenchStore.getState().setPendingSeek(Math.max(0, t));
      setMainSignal((n) => n + 1);
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
        const video = mainElRef.current;
        if (!video) {
          setMainSignal((n) => n + 1);
          return;
        }
        if (video.paused) void video.play().catch(() => {});
        else video.pause();
      },
      pause: () => mainElRef.current?.pause(),
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
  return (
    <section data-slot="marketing-stage" className={STAGE_CLASS}>
      {playable && (
        <div className="flex flex-wrap items-center gap-2 px-3 pt-3">
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
        {playable && (
          <figure className="relative flex h-full min-w-0 flex-col items-center">
            <div className={PLAYER_BOX} style={{ aspectRatio: `${w} / ${h}` }}>
              <VideoPlayer
                key={mainSrc}
                src={mainSrc}
                durationMs={mainTotal * 1000}
                onVideo={onMainVideo}
                playSignal={mainSignal}
              />
            </div>
          </figure>
        )}
      </div>
    </section>
  );
}

/** 播放器外框：按成片画幅，填满舞台高度；共享播放器填满它 */
const PLAYER_BOX =
  "relative min-h-0 flex-1 overflow-hidden rounded-[22px] bg-black shadow-[0_14px_40px_-18px_oklch(0_0_0/0.7)]";

const STAGE_CLASS =
  "bg-chrome ring-chrome-border flex min-h-0 flex-col overflow-hidden rounded-[28px] ring-1 backdrop-blur-xl max-lg:min-h-[70svh]";
