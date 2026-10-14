import { memo } from "react";
import { ViewportPortal, useStore, type ReactFlowState } from "@xyflow/react";

import { useAlignGuidesStore } from "@/store/align-guides";
import type { Guide } from "@/utils/canvas/alignment";

const zoomOf = (state: ReactFlowState) => state.transform[2];

/** 线宽、点距都是屏幕像素，除以缩放换成画布坐标，缩放后看起来一样细 */
const STROKE_PX = 0.75;
const DASH_PX = [2, 2];

/** 只有出现辅助线才挂载，这样没在拖动时不订阅缩放，滚轮缩放不会白白渲染它 */
function GuideLines({ guides }: { guides: Guide[] }) {
  const zoom = Math.max(useStore(zoomOf), 0.01);
  return (
    <ViewportPortal>
      <svg
        width={1}
        height={1}
        aria-hidden="true"
        className="pointer-events-none absolute top-0 left-0 overflow-visible"
      >
        {guides.map((guide) => {
          const vertical = guide.axis === "x";
          return (
            <line
              key={`${guide.axis}-${guide.value}`}
              x1={vertical ? guide.value : guide.from}
              y1={vertical ? guide.from : guide.value}
              x2={vertical ? guide.value : guide.to}
              y2={vertical ? guide.to : guide.value}
              strokeWidth={STROKE_PX / zoom}
              strokeDasharray={DASH_PX.map((px) => px / zoom).join(" ")}
              style={{ stroke: "var(--muted-foreground)" }}
            />
          );
        })}
      </svg>
    </ViewportPortal>
  );
}

/**
 * 拖动节点时的对齐辅助线：放在跟节点同一坐标系的覆盖层里，随平移缩放；
 * 只是视觉，不进 nodes、不保存、不拦截指针。线只画在两个节点之间的空隙里，细点线。
 */
export const AlignGuidesLayer = memo(function AlignGuidesLayer() {
  const guides = useAlignGuidesStore((state) => state.guides);
  return guides.length === 0 ? null : <GuideLines guides={guides} />;
});
