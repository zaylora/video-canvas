import { useEffect, useRef, useState } from "react";
import { Maximize2, Minimize2, Plus, Video } from "lucide-react";

import { cn } from "@/lib/utils";
import { useDirectorStore } from "@/store/director";
import type { CameraPose, DirectorCamera } from "@/types";
import { ASPECT_RATIO } from "@/utils/director/export";
import { createShotRenderer } from "@/utils/director/render";
import { canAdd, limitMessage } from "@/utils/director/scene";

/** 没有场景时的机位列表；用常量，选择器每次返回同一个引用 */
const NO_CAMERAS: DirectorCamera[] = [];

/** 小窗画面的宽度：平时 / 放大后（像素） */
const WIDTH = { small: 296, large: 560 } as const;
/** 竖画幅时画面最高多少，免得小窗把视口占满 */
const MAX_HEIGHT = { small: 236, large: 420 } as const;

/**
 * 机位取景小窗：当前机位的实时画面，和导出画面一致（同画幅、同视场角、不带网格和标签）。
 * 顶部切换机位、新建机位；右上角放大 / 缩回。
 */
export function DirectorPreview() {
  const hostRef = useRef<HTMLDivElement>(null);
  const [large, setLarge] = useState(false);
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
  const mode = large ? "large" : "small";
  let width: number = WIDTH[mode];
  let height = Math.round(width / ratio);
  if (height > MAX_HEIGHT[mode]) {
    height = MAX_HEIGHT[mode];
    width = Math.round(height * ratio);
  }

  // 自己的离屏渲染器，每帧按编辑器状态出一张图（带着临时姿态和拖动中的机位）。
  // 只在尺寸变化时重建：拖动机位时机位对象每帧都是新的，不能拿它当依赖
  useEffect(() => {
    const host = hostRef.current;
    if (!host || !hasActive) return;
    const shot = createShotRenderer();
    shot.canvas.style.width = `${width}px`;
    shot.canvas.style.height = `${height}px`;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    shot.setSize(Math.round(width * dpr), Math.round(height * dpr));
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
      shot.canvas.remove();
      shot.dispose();
    };
  }, [hasActive, height, width]);

  return (
    <div className="bg-card border-border absolute right-3 bottom-3 overflow-hidden rounded-xl border shadow-lg">
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
          title={large ? "缩小" : "放大"}
          onClick={() => setLarge((value) => !value)}
          className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring ml-auto inline-flex size-6 shrink-0 items-center justify-center rounded-md outline-none focus-visible:ring-2"
        >
          {large ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
        </button>
      </div>
      {active ? (
        <>
          <div className="text-muted-foreground flex gap-1.5 px-2 py-1.5 text-[11px] tabular-nums">
            <span className="bg-muted rounded px-1.5">FOV {fov}°</span>
            <span className="bg-muted rounded px-1.5">{active.aspect}</span>
            <span className="bg-muted rounded px-1.5">{t.toFixed(2)}s</span>
          </div>
          <div ref={hostRef} className="bg-black" style={{ width, height }} />
        </>
      ) : (
        <div className="text-muted-foreground flex w-[296px] flex-col items-center gap-2 py-8 text-xs">
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
