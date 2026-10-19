import { useEffect, useRef, useState } from "react";
import { Columns2, PictureInPicture2, Plus, Video } from "lucide-react";

import { cn } from "@/lib/utils";
import { useDirectorStore } from "@/store/director";
import type { CameraPose, DirectorCamera } from "@/types";
import { ASPECT_RATIO } from "@/utils/director/export";
import { fitFrame, floatingFrame } from "@/utils/director/layout";
import { createShotRenderer } from "@/utils/director/render";
import { canAdd, limitMessage } from "@/utils/director/scene";

/** 没有场景时的机位列表；用常量，选择器每次返回同一个引用 */
const NO_CAMERAS: DirectorCamera[] = [];

/** 分栏时画面四周留的空白（像素） */
const SPLIT_PADDING = 16;

/**
 * 机位取景画面：当前机位的实时画面，和导出画面一致（同画幅、同视场角、不带网格和标签）。
 * 顶部切换机位、新建机位；右上角在两种摆法间切换：
 * 浮窗（视口右下角的小窗）和左右分栏（右半边整块是机位画面，画面按画幅居中放大）。
 * 两种摆法是同一棵元素树、只换样式，所以切换时渲染器不重建。
 */
export function DirectorPreview() {
  const hostRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const shotRef = useRef<ReturnType<typeof createShotRenderer> | null>(null);
  const split = useDirectorStore((state) => state.previewSplit);
  /** 分栏时画面区的大小（含留白） */
  const [box, setBox] = useState({ width: 0, height: 0 });
  const cameras = useDirectorStore((state) => state.scene?.cameras ?? NO_CAMERAS);
  const activeId = useDirectorStore((state) => state.scene?.activeCameraId ?? "");
  const canAddCamera = useDirectorStore((state) => !!state.scene && canAdd(state.scene, "camera"));
  const active = cameras.find((c) => c.id === activeId);
  const fov = useDirectorStore((state) =>
    active ? Math.round((state.poseOf(active.id) as CameraPose | undefined)?.fov ?? active.fov) : 0,
  );
  const t = useDirectorStore((state) => state.t);

  const hasActive = !!active;
  const ratio = active ? ASPECT_RATIO[active.aspect] : 16 / 9;
  const { width, height } = split
    ? fitFrame(box.width - SPLIT_PADDING * 2, box.height - SPLIT_PADDING * 2, ratio)
    : floatingFrame(ratio);

  // 分栏时量画面区的大小，窗口、窗格变化时跟着变
  useEffect(() => {
    const body = bodyRef.current;
    if (!split || !body) return;
    const measure = () => setBox({ width: body.clientWidth, height: body.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(body);
    return () => observer.disconnect();
  }, [split, hasActive]);

  // 自己的离屏渲染器，每帧按编辑器状态出一张图（带着临时姿态和拖动中的机位）。
  // 只在有没有机位变化时重建；尺寸变化走下面那个 effect 调整，不重建渲染器。
  // 拖动机位时机位对象每帧都是新的，不能拿它当依赖
  useEffect(() => {
    const host = hostRef.current;
    if (!host || !hasActive) return;
    const shot = createShotRenderer();
    shotRef.current = shot;
    host.appendChild(shot.canvas);
    let frame = 0;
    const draw = () => {
      const state = useDirectorStore.getState();
      const camera = state.scene?.cameras.find((c) => c.id === state.scene?.activeCameraId);
      if (state.scene && camera) {
        shot.renderPose(
          state.scene,
          state.poseOf,
          (state.poseOf(camera.id) ?? camera) as CameraPose,
        );
      }
      frame = requestAnimationFrame(draw);
    };
    frame = requestAnimationFrame(draw);
    return () => {
      cancelAnimationFrame(frame);
      shotRef.current = null;
      shot.canvas.remove();
      shot.dispose();
    };
  }, [hasActive]);

  useEffect(() => {
    const shot = shotRef.current;
    if (!shot || !(width > 0)) return;
    shot.canvas.style.width = `${width}px`;
    shot.canvas.style.height = `${height}px`;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    shot.setSize(Math.round(width * dpr), Math.round(height * dpr));
  }, [hasActive, height, width]);

  return (
    <div
      className={cn(
        "bg-card border-border overflow-hidden",
        split
          ? "absolute inset-y-0 right-0 flex w-1/2 flex-col border-l"
          : "absolute right-3 bottom-3 rounded-xl border shadow-lg",
      )}
    >
      <div className="border-border flex items-center gap-1 overflow-x-auto border-b p-1.5">
        {cameras.map((camera) => (
          <button
            key={camera.id}
            type="button"
            onClick={() => useDirectorStore.getState().select(camera.id)}
            className={cn(
              "text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-2 text-xs outline-none focus-visible:ring-2",
              camera.id === activeId && "bg-muted text-foreground",
            )}
          >
            <Video className="size-3" />
            {camera.name}
          </button>
        ))}
        <button
          type="button"
          title={canAddCamera ? "新建机位" : limitMessage("camera")}
          disabled={!canAddCamera}
          onClick={() => useDirectorStore.getState().addObject("camera")}
          className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring inline-flex size-6 shrink-0 items-center justify-center rounded-md outline-none focus-visible:ring-2 disabled:opacity-40"
        >
          <Plus className="size-3.5" />
        </button>
        <button
          type="button"
          title={split ? "收成浮窗" : "左右分栏"}
          aria-label={split ? "收成浮窗" : "左右分栏"}
          onClick={() => useDirectorStore.getState().toggleSplit()}
          className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring ml-auto inline-flex size-6 shrink-0 items-center justify-center rounded-md outline-none focus-visible:ring-2"
        >
          {split ? <PictureInPicture2 className="size-3.5" /> : <Columns2 className="size-3.5" />}
        </button>
      </div>
      {active ? (
        <>
          <div className="text-muted-foreground flex gap-1.5 px-2 py-1.5 text-[11px] tabular-nums">
            <span className="bg-muted rounded px-1.5">FOV {fov}°</span>
            <span className="bg-muted rounded px-1.5">{active.aspect}</span>
            <span className="bg-muted rounded px-1.5">{t.toFixed(2)}s</span>
          </div>
          <div ref={bodyRef} className={cn(split && "grid min-h-0 flex-1 place-items-center")}>
            <div ref={hostRef} className="bg-black" style={{ width, height }} />
          </div>
        </>
      ) : (
        <div
          className={cn(
            "text-muted-foreground flex flex-col items-center gap-2 py-8 text-xs",
            split ? "flex-1 justify-center" : "w-[296px]",
          )}
        >
          还没有机位
          <button
            type="button"
            disabled={!canAddCamera}
            onClick={() => useDirectorStore.getState().addObject("camera")}
            className="bg-muted text-foreground hover:bg-muted/70 inline-flex h-7 items-center gap-1 rounded-md px-2.5"
          >
            <Plus className="size-3.5" />
            添加一台机位
          </button>
        </div>
      )}
    </div>
  );
}
