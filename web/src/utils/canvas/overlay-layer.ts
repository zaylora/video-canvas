/**
 * 盖在被选中节点上的覆盖层（涂鸦画布、宫格切分线）的层级。
 * xyflow 给选中节点的 zIndex 加 1000，这类编辑态下节点一定是选中的；
 * 覆盖层放在 ViewportPortal 里，不设层级就会被节点压在下面：看不见，也点不到。
 */
export const SELECTED_NODE_OVERLAY_Z_INDEX = 1100;
