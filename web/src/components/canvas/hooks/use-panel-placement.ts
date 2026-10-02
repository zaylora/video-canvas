import { useCallback } from "react";
import { Position, useStore, type ReactFlowState } from "@xyflow/react";

/** 面板宽度：跟着节点在屏幕上的宽度走，但不随缩放变得太窄或太宽 */
const PANEL_MIN_WIDTH = 480;
const PANEL_MAX_WIDTH = 720;
/** 面板比节点两边各多出这么多 */
const PANEL_EXTRA_WIDTH = 160;
/** 离视口边缘至少留这么多 */
const VIEWPORT_GUTTER = 12;
/** 底部工具条占的高度，面板放下面时不能压住它 */
const BOTTOM_CHROME = 80;
/** 节点上方的标题行和历史浮条占的高度 */
const TOP_CHROME = 96;
/** 面板与节点的间距 */
export const PANEL_OFFSET = 16;

export type PanelPlacement = {
  /** 面板挂在节点下方还是上方 */
  position: Position.Bottom | Position.Top;
  /** 面板宽度（屏幕像素） */
  width: number;
  /** 水平方向要挪多少才不出视口 */
  shift: number;
};

const DEFAULT_PLACEMENT = `${Position.Bottom}|${PANEL_MIN_WIDTH}|0`;

/**
 * 生成面板的摆法（设计稿 6.4）：
 * 宽度 = clamp(480, 节点屏幕宽 + 160, 720)；下方放不下、上方更宽裕时翻到上方；
 * 左右贴住视口边缘留 12px。选择器返回字符串，只有摆法真的变了才重渲染。
 */
export function usePanelPlacement(nodeId: string, panelHeight: number): PanelPlacement {
  const selector = useCallback(
    (state: ReactFlowState) => {
      const node = state.nodeLookup.get(nodeId);
      if (!node) return DEFAULT_PLACEMENT;
      const [tx, ty, zoom] = state.transform;
      const width = (node.measured.width ?? 384) * zoom;
      const height = (node.measured.height ?? 216) * zoom;
      const left = node.internals.positionAbsolute.x * zoom + tx;
      const top = node.internals.positionAbsolute.y * zoom + ty;

      const panelWidth = Math.min(
        state.width - VIEWPORT_GUTTER * 2,
        Math.max(PANEL_MIN_WIDTH, Math.min(PANEL_MAX_WIDTH, width + PANEL_EXTRA_WIDTH)),
      );
      const below = state.height - (top + height) - PANEL_OFFSET - BOTTOM_CHROME;
      const above = top - PANEL_OFFSET - TOP_CHROME;
      const flip = below < panelHeight && above > below;

      const panelLeft = left + width / 2 - panelWidth / 2;
      const panelRight = panelLeft + panelWidth;
      const shift =
        panelLeft < VIEWPORT_GUTTER
          ? VIEWPORT_GUTTER - panelLeft
          : panelRight > state.width - VIEWPORT_GUTTER
            ? state.width - VIEWPORT_GUTTER - panelRight
            : 0;

      return `${flip ? Position.Top : Position.Bottom}|${Math.round(panelWidth)}|${Math.round(shift)}`;
    },
    [nodeId, panelHeight],
  );
  const [position, width, shift] = useStore(selector).split("|");
  return {
    position: position as PanelPlacement["position"],
    width: Number(width),
    shift: Number(shift),
  };
}

/** 画布正在被拖或框选：浮层淡下去、不吃指针，免得挡住视线 */
export function usePaneBusy() {
  return useStore((state: ReactFlowState) => state.paneDragging || state.userSelectionActive);
}

/** 这个节点自己正在被拖：浮层整个收起，松手再出来 */
export function useNodeDragging(nodeId: string) {
  return useStore(
    useCallback((state: ReactFlowState) => !!state.nodeLookup.get(nodeId)?.dragging, [nodeId]),
  );
}
