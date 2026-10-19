import { toast } from "sonner";

import { useDirectorStore } from "@/store/director";
import { useFramePickStore } from "@/store/frame-pick";
import { useOutpaintStore } from "@/store/outpaint";
import { useTrimStore } from "@/store/trim";
import type { CanvasNodeData } from "@/types";
import { createDefaultScene } from "@/utils/director/scene";
import { supportsWebGL2 } from "@/utils/director/webgl";

/** 浏览器不支持 3D 时的提示 */
export const NO_WEBGL_MESSAGE = "当前浏览器不支持 3D（WebGL2），请用新版 Chrome / Edge 打开导演台";

/**
 * 打开导演台编辑器（双击节点、功能区「打开导演台 / 导出视频」都走这里）。
 * 不支持 WebGL2 时不打开、说明原因；打开前先退出扩图、修剪、选帧，几种模式互斥。
 * @param options.exportOnOpen 打开后直接弹导出对话框
 */
export function openDirector(
  nodeId: string,
  data: CanvasNodeData,
  options?: { exportOnOpen?: boolean },
) {
  if (!supportsWebGL2()) {
    toast(NO_WEBGL_MESSAGE);
    return;
  }
  const store = useDirectorStore.getState();
  if (store.nodeId === nodeId) return;
  if (store.exporting) {
    toast("另一个导演台正在导出，请先等它完成");
    return;
  }
  useOutpaintStore.getState().leave();
  useTrimStore.getState().leave();
  useFramePickStore.getState().leave();
  store.open(nodeId, data.scene ?? createDefaultScene(), options);
}
