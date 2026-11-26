import { lazy, Suspense, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { useDirectorStore } from "@/store/director";
import type { DirectorScene } from "@/types";
import { loadFigureModels } from "@/utils/director/figure";
import { sceneModels } from "@/utils/director/preload";

const DirectorViewport = lazy(() =>
  import("./director-viewport").then((module) => ({ default: module.DirectorViewport })),
);

const PREVIEW_SESSION = "shared-preview";

function LoadingScene() {
  return (
    <p role="status" className="p-4">
      正在加载场景…
    </p>
  );
}

export default function SharedDirectorPreview({ scene }: { scene: DirectorScene }) {
  const [ready, setReady] = useState(false);
  const time = useDirectorStore((state) => state.t);
  const playing = useDirectorStore((state) => state.playing);
  const camera = useDirectorStore((state) =>
    state.viewMode === "camera" ? state.scene?.activeCameraId : "",
  );

  useEffect(() => {
    const director = useDirectorStore.getState();
    if (director.nodeId !== PREVIEW_SESSION) {
      director.open(PREVIEW_SESSION, scene);
      return;
    }
    const cameraId = director.viewMode === "camera" ? director.scene?.activeCameraId : undefined;
    const keepCamera = !!cameraId && scene.cameras.some((camera) => camera.id === cameraId);
    useDirectorStore.setState({
      scene: keepCamera ? { ...scene, activeCameraId: cameraId } : scene,
      t: Math.min(director.t, scene.duration),
      playing: director.playing && director.t < scene.duration,
      viewMode: keepCamera ? "camera" : "director",
      selectedId: keepCamera ? cameraId : null,
    });
  }, [scene]);

  useEffect(
    () => () => {
      const director = useDirectorStore.getState();
      if (director.nodeId === PREVIEW_SESSION) director.reset();
    },
    [],
  );

  useEffect(() => {
    let active = true;
    const showScene = () => {
      if (active) setReady(true);
    };
    void loadFigureModels(sceneModels(scene)).then(showScene, showScene);
    return () => {
      active = false;
    };
  }, [scene]);

  useEffect(() => {
    if (!playing) return;
    let frame = 0;
    let last = performance.now();
    const advance = (now: number) => {
      const state = useDirectorStore.getState();
      const next = state.t + (now - last) / 1000;
      last = now;
      state.setTime(next);
      if (next >= scene.duration) state.setPlaying(false);
      else frame = requestAnimationFrame(advance);
    };
    frame = requestAnimationFrame(advance);
    return () => cancelAnimationFrame(frame);
  }, [playing, scene.duration]);

  const toggle = () => {
    const state = useDirectorStore.getState();
    if (state.t >= scene.duration) state.setTime(0);
    state.setPlaying(!state.playing);
  };

  return (
    <div className="space-y-3">
      <div className="bg-muted relative h-[min(56dvh,36rem)] overflow-hidden rounded-lg">
        {ready ? (
          <Suspense fallback={<LoadingScene />}>
            <DirectorViewport readOnly />
          </Suspense>
        ) : (
          <LoadingScene />
        )}
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button size="sm" variant="outline" disabled={!ready} onClick={toggle}>
          {playing ? "暂停" : "播放"}
        </Button>
        <input
          type="range"
          aria-label="播放进度"
          className="min-w-32 flex-1"
          min={0}
          max={scene.duration}
          step={0.01}
          value={time}
          disabled={!ready}
          onChange={(event) => useDirectorStore.getState().setTime(Number(event.target.value))}
        />
        <span className="text-muted-foreground text-xs tabular-nums">
          {time.toFixed(1)} / {scene.duration.toFixed(1)} 秒
        </span>
        <select
          aria-label="查看机位"
          value={camera}
          className="bg-muted max-w-40 rounded-md p-1"
          onChange={(event) => {
            const director = useDirectorStore.getState();
            if (event.target.value) director.enterCameraView(event.target.value);
            else director.setViewMode("director");
          }}
        >
          <option value="">导演视角</option>
          {scene.cameras.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}
