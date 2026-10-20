import type { SyntheticEvent } from "react";

/** 只用到 stopPropagation，测试里传个假事件就行 */
const stop = (event: Pick<SyntheticEvent, "stopPropagation">) => event.stopPropagation();

/**
 * 涂鸦画布外层要拦住的手势：按下、点击、双击、右键、滚轮都别冒泡到 xyflow 的画布 / 节点上，
 * 否则会拖节点、框选、点空白取消选中。
 *
 * 不能拦松开和移动（mouseup、mousemove、pointerup、pointermove、pointercancel）：
 * Fabric 在按下后把它们挂在 document 上，React 的 stopPropagation 会让事件停在根节点，
 * 到不了 document，Fabric 收不到松开，这一笔就一直画下去。
 */
export const doodleStageGuards = {
  onPointerDown: stop,
  onMouseDown: stop,
  onClick: stop,
  onDoubleClick: stop,
  onContextMenu: stop,
  onWheel: stop,
};
