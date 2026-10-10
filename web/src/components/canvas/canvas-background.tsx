import { Background, useStore } from "@xyflow/react";

import {
  BACKGROUND_DOT_MIN_ZOOM,
  BACKGROUND_DOT_SIZE,
  BACKGROUND_VARIANTS,
} from "@/constants/canvas";
import { GRID_SIZE, type CanvasBackground } from "@/store";

/**
 * 点阵圆点的直径按缩放反向补偿：xyflow 的圆点会随画布缩放一起放大缩小，
 * 除以缩放比后，屏幕上的点始终是 BACKGROUND_DOT_SIZE 那么大。
 * 缩放低于 BACKGROUND_DOT_MIN_ZOOM 时返回 null：点距缩到几像素，点阵会糊成一片灰。
 * Background 自己就订阅了缩放，这里多订一份只在这个小组件里重渲染，不牵连整张画布。
 */
const useDotSize = () =>
  useStore((state) => {
    const zoom = state.transform[2];
    return zoom < BACKGROUND_DOT_MIN_ZOOM ? null : BACKGROUND_DOT_SIZE / zoom;
  });

/** 画布底纹：点阵、网格线，"none" 不画 */
export function CanvasBackgroundLayer({ background }: { background: CanvasBackground }) {
  const dotSize = useDotSize();
  if (background === "none") return null;
  if (background === "dots" && dotSize === null) return null;
  return (
    <Background
      variant={BACKGROUND_VARIANTS[background]}
      gap={GRID_SIZE}
      // size 只对点阵生效；网格线用默认线宽
      size={background === "dots" ? (dotSize ?? undefined) : undefined}
    />
  );
}
