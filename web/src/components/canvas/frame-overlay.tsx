import { useEffect } from "react";
import { motion } from "motion/react";
import { NodeToolbar, Position } from "@xyflow/react";
import { toast } from "sonner";

import { FramePanel } from "@/components/canvas/frame-panel";
import {
  PANEL_OFFSET,
  usePaneBusy,
  usePanelPlacement,
} from "@/components/canvas/hooks/use-panel-placement";
import { useFrameNodes } from "@/hooks/use-frame-nodes";
import { useTrimSource } from "@/hooks/use-trim-source";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useFramePickStore } from "@/store/frame-pick";
import type { CanvasNodeData } from "@/types";

/**
 * 选帧模式：节点下方的选帧面板，取代平时的功能区和生成面板；节点正文换成预览播放器由节点自己决定。
 * 这里负责：读源视频的时长和缩略图、Esc 退出、点确认后把暂存的帧建成图片节点。
 * 取消选中、多选时退出由节点自己处理（和修剪一样）。
 */
export function FrameOverlay({ id, data }: { id: string; data: CanvasNodeData }) {
  const { width, shift } = usePanelPlacement(id);
  const paneBusy = usePaneBusy();
  const source = useTrimSource(data.src ?? undefined);
  const { placeFrames } = useFrameNodes();

  // 读到时长：写进仓库，播放头和时间显示才有数
  useEffect(() => {
    if (source.status === "ready") {
      useFramePickStore.getState().setDuration(source.probe.duration);
    }
  }, [source]);

  // Esc 退出
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") useFramePickStore.getState().leave(id);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [id]);

  const confirm = (files: File[]) => {
    const placed = placeFrames(id, files);
    if (placed === 0) toast("视频已经不在画布上，帧没有保存");
    useFramePickStore.getState().reset();
  };

  return (
    <NodeToolbar isVisible position={Position.Bottom} offset={PANEL_OFFSET}>
      <motion.div
        initial={{ opacity: 0, y: -6, scale: 0.985 }}
        animate={{ opacity: paneBusy ? 0.35 : 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        style={{ translate: `${shift}px 0`, pointerEvents: paneBusy ? "none" : undefined }}
      >
        <FramePanel
          width={width}
          src={data.src ?? ""}
          label={data.label}
          thumbs={source.status === "ready" ? source.thumbs : []}
          loadError={source.status === "error" ? source.message : undefined}
          onConfirm={confirm}
          onClose={() => useFramePickStore.getState().leave(id)}
        />
      </motion.div>
    </NodeToolbar>
  );
}
