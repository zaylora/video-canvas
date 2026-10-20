import type { SyntheticEvent } from "react";

/** 只用到 stopPropagation，测试里传个假事件就行 */
const stop = (event: Pick<SyntheticEvent, "stopPropagation">) => event.stopPropagation();

/**
 * 画布里弹窗（DialogContent）要拦住的手势。
 * 弹窗经 Portal 渲染到 body，但 React 的事件按组件树冒泡，而弹窗挂在节点的组件树里：
 * 不拦的话，弹窗里的双击会冒到节点和画布上，打开节点预览、在空白处新建节点；
 * 点击会选中节点或取消选中，右键会弹出画布菜单。
 * 不拦键盘事件：Esc 关弹窗靠它。
 */
export const canvasDialogGuards = {
  onClick: stop,
  onDoubleClick: stop,
  onPointerDown: stop,
  onMouseDown: stop,
  onContextMenu: stop,
};
