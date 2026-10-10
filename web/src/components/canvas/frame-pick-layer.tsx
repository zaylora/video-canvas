import { useNodeId } from "@xyflow/react";

import { useFramePickStore } from "@/store/frame-pick";

/**
 * 视频节点画幅里的选帧预览层：只在这个节点正在「截取帧 → 自定义」时出现，
 * 盖在封面和 video 上面，面板拖播放头时往里画对应的那一帧。
 * 画布本身不画任何东西，只把自己登记进 store，等面板来画；它盖住封面上的播放钮，选帧时不会误点播放。
 */
export function FramePickLayer() {
  const nodeId = useNodeId();
  const picking = useFramePickStore((state) => nodeId !== null && state.nodeId === nodeId);
  const setCanvas = useFramePickStore((state) => state.setCanvas);
  if (!picking) return null;
  return (
    <canvas
      ref={setCanvas}
      aria-hidden
      className="absolute inset-0 size-full bg-black object-contain"
    />
  );
}
