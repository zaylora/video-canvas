import type { CSSProperties } from "react";
import { useViewTransitionState } from "react-router";

import { canvasTransitionName } from "@/utils/home/canvas-transition";

/**
 * 画布卡参与「首页 → 所有画布」转场用的样式：只在转场进行中返回 view-transition-name，
 * 平时返回 undefined，不产生额外的合成层，也不会因为同名元素让浏览器放弃转场。
 * 首页和所有画布页的卡片都用它，同一个 key 就是同一个共享元素。
 * @param key 画布 id；新建画布卡传 "new"
 * @param enabled false 时不参与（所有画布页里没有对应首页卡的那些）
 */
export function useCanvasTransitionStyle(key: string, enabled = true): CSSProperties | undefined {
  const transitioning = useViewTransitionState("/canvases");
  return transitioning && enabled ? { viewTransitionName: canvasTransitionName(key) } : undefined;
}
