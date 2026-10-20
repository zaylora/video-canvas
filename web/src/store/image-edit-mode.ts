import { useDoodleStore } from "./doodle";
import { useGridSplitStore } from "./grid-split";
import { useOutpaintStore } from "./outpaint";

/**
 * 退出图片节点上所有的编辑态（扩图、宫格切分、涂鸦）。
 * 这三种编辑态同一时刻最多一个：进入其中任何一个之前先调它，不管它们在哪个节点上。
 */
export function leaveImageEditModes(): void {
  useOutpaintStore.getState().leave();
  useGridSplitStore.getState().leave();
  useDoodleStore.getState().leave();
}
