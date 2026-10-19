import { useEffect, useRef, useState } from "react";

import { useDirectorStore } from "@/store/director";
import type { CameraPose } from "@/types";
import { activeCameraOf } from "@/utils/director/camera-view";
import { ASPECT_RATIO } from "@/utils/director/export";
import { fitFrame } from "@/utils/director/layout";

/**
 * 机位视角下盖在视口上的取景框：框内就是这台机位导出时拍到的范围，框外压暗；
 * 左上角写机位名、画幅和视场角。已有关键帧的机位被拖动后，框顶上提示「未记录」。
 * 导演视角下什么都不画。
 */
export function CameraFrame() {
  const hostRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState({ width: 0, height: 0 });
  const camId = useDirectorStore((state) => activeCameraOf(state.scene, state.viewMode));
  const camera = useDirectorStore((state) => state.scene?.cameras.find((c) => c.id === camId));
  const fov = useDirectorStore((state) =>
    camId ? Math.round((state.poseOf(camId) as CameraPose | undefined)?.fov ?? 0) : 0,
  );
  const unrecorded = useDirectorStore((state) => !!camId && !!state.pending[camId]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host || !camId) return;
    const measure = () => setBox({ width: host.clientWidth, height: host.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(host);
    return () => observer.disconnect();
  }, [camId]);

  if (!camId || !camera) return null;
  const frame = fitFrame(box.width, box.height, ASPECT_RATIO[camera.aspect]);

  return (
    <div ref={hostRef} className="pointer-events-none absolute inset-0 overflow-hidden">
      {frame.width > 0 && (
        <div
          data-camera-frame
          className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 border border-white/60"
          // 框外压暗：超大的外阴影，被外层 overflow-hidden 裁掉
          style={{
            width: frame.width,
            height: frame.height,
            boxShadow: "0 0 0 9999px rgb(0 0 0 / 0.5)",
          }}
        >
          <span className="bg-background/70 text-foreground absolute top-2 left-2 rounded px-1.5 py-0.5 text-[11px] tabular-nums">
            {camera.name} · {camera.aspect} · FOV {fov}°
          </span>
          {unrecorded && (
            <span className="text-status-running bg-background/70 absolute top-2 left-1/2 -translate-x-1/2 rounded px-1.5 py-0.5 text-[11px]">
              未记录 · 按 K 记录关键帧
            </span>
          )}
        </div>
      )}
    </div>
  );
}
