import type { NodeProps } from "@xyflow/react";

import type { CanvasNode } from "@/types";

/** 节点视图实际用到的 props；以后视图要读别的（比如尺寸），必须同时把它加进下面的比较 */
type ViewProps = Pick<NodeProps<CanvasNode>, "id" | "data" | "selected">;

/**
 * 节点视图只读 id、data、selected。xyflow 拖动时每帧都会换 positionAbsoluteX/Y 和 dragging，
 * 默认的浅比较会让被拖节点每帧整棵子树重渲染；位置由外层容器用 transform 更新，视图不需要知道。
 */
export const sameNodeProps = (prev: ViewProps, next: ViewProps) =>
  prev.id === next.id && prev.data === next.data && prev.selected === next.selected;
