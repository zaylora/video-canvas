import { useCallback } from "react";
import { useStore, type ReactFlowState } from "@xyflow/react";

import { PANEL_WIDTH, panelLayout } from "@/utils/canvas/panel-layout";
import { DEFAULT_NODE_SIZE } from "@/utils/canvas/placement";

/** 面板与节点的间距 */
export const PANEL_OFFSET = 16;

export type PanelPlacement = {
  /** 面板宽度（屏幕像素） */
  width: number;
  /** 水平方向要挪多少才不出视口 */
  shift: number;
};

const DEFAULT_PLACEMENT = `${PANEL_WIDTH}|0`;

/**
 * 生成面板的摆法（设计稿 6.4）：
 * 宽度固定 680，不随画布缩放变化；左右贴住视口边缘留 12px，算法见 panelLayout。
 * 面板始终在节点下方，下方空间不够时由画布平移去让，不翻到上方（位置不跳）。
 * 选择器返回字符串，只有摆法真的变了才重渲染。
 */
export function usePanelPlacement(nodeId: string): PanelPlacement {
  const selector = useCallback(
    (state: ReactFlowState) => {
      const node = state.nodeLookup.get(nodeId);
      if (!node) return DEFAULT_PLACEMENT;
      const [tx, , zoom] = state.transform;
      const { width, shift } = panelLayout({
        nodeWidth: (node.measured.width ?? DEFAULT_NODE_SIZE.width) * zoom,
        nodeLeft: node.internals.positionAbsolute.x * zoom + tx,
        viewportWidth: state.width,
      });

      return `${Math.round(width)}|${Math.round(shift)}`;
    },
    [nodeId],
  );
  const [width, shift] = useStore(selector).split("|");
  return {
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
