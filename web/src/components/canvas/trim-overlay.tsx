import { useEffect } from "react";
import { motion } from "motion/react";
import { NodeToolbar, Position, useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import { TrimPanel } from "@/components/canvas/trim-panel";
import {
  PANEL_OFFSET,
  usePaneBusy,
  usePanelPlacement,
} from "@/components/canvas/hooks/use-panel-placement";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { useTrimSource } from "@/hooks/use-trim-source";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useTrimStore } from "@/store/trim";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { buildTrimNode, trimFileName, trimUrl } from "@/utils/canvas/trim";
import { trimRunner } from "@/utils/canvas/trim-runner";
import { uploadRunner } from "@/utils/canvas/upload-runner";

/**
 * 读源文件大小（字节），用来在编码前粗估产物会不会超过上传上限。
 * 读不到（跨域不暴露 Content-Length、网络出错）返回 0：预估放行，编码完仍会按真实体积再拦一次。
 */
async function sourceSize(src: string): Promise<number> {
  try {
    const response = await fetch(trimUrl(src), { method: "HEAD", mode: "cors" });
    return Number(response.headers.get("content-length")) || 0;
  } catch {
    return 0;
  }
}

/**
 * 修剪模式（设计稿「视频修剪」）：节点下方的修剪面板，取代平时的功能区和生成面板；
 * 节点正文换成预览播放器由节点自己根据 useTrimStore 决定。
 * 这里负责：读源视频的时长和缩略图、点 ✓ 后编码、编码成功就建「剪辑」节点和连线并上传。
 * 编码任务在模块级的 trimRunner 里，面板卸载（取消选中）不会中止它，只有 × 确认才会。
 */
export function TrimOverlay({ id, data }: { id: string; data: CanvasNodeData }) {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const history = useCanvasHistoryContext();
  const { width, shift } = usePanelPlacement(id);
  const paneBusy = usePaneBusy();
  const source = useTrimSource(data.src ?? undefined);

  // 读到时长就进入编辑态；面板重新挂上（编码中取消选中又选回来）时 ready 不会重置选区
  useEffect(() => {
    if (source.status === "ready") {
      useTrimStore.getState().ready(source.probe.duration, source.probe.hasAudio);
    }
  }, [source]);

  // 读取失败：仍在 loading 的话落回编辑态，让手柄可拖；✓ 靠 loadError 禁用
  useEffect(() => {
    if (source.status === "error" && useTrimStore.getState().phase === "loading") {
      useTrimStore.getState().setPhase("editing");
    }
  }, [source]);

  const confirm = async () => {
    const store = useTrimStore.getState();
    if (source.status !== "ready" || !data.src) return;
    const origin = getNode(id);
    if (!origin) return;
    const result = await trimRunner.start({
      nodeId: id,
      src: data.src,
      start: store.start,
      end: store.end,
      duration: store.duration,
      discardAudio: store.discardAudio,
      sourceBytes: await sourceSize(data.src),
      fileName: trimFileName(data.label, "mp4"),
    });
    if (result.outcome !== "done") return;

    // 编码完成：建节点、连线算一步撤销，然后退出模式，上传走现成流程
    const latest = getNode(id);
    if (!latest) {
      useTrimStore.getState().reset();
      return;
    }
    const { probe } = source;
    const aspect = probe.width > 0 && probe.height > 0 ? probe.width / probe.height : 16 / 9;
    history?.record();
    const { node, edge } = buildTrimNode({
      source: latest,
      nodes: getNodes(),
      fileName: result.file.name,
      aspect,
    });
    setNodes((nodes) => [...nodes, node]);
    setEdges((edges) => [...edges, edge]);
    useTrimStore.getState().reset();
    void uploadRunner
      .start(node.id, result.file, (nodeId, patch) => updateNodeData(nodeId, patch))
      .then((outcome) => {
        if (outcome === "failed") toast.error("剪辑上传失败，可以在节点上重试");
      });
  };

  const close = () => {
    const store = useTrimStore.getState();
    if (store.phase !== "encoding") {
      store.reset();
      return;
    }
    // 编码中点 ×：二次确认后才中止
    if (window.confirm("中止编码？已编码的部分不会保留。")) {
      trimRunner.abort(id);
      useTrimStore.getState().reset();
    }
  };

  // Esc 退出（编码中无效，只有 × 能中止）
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      const store = useTrimStore.getState();
      if (store.phase === "encoding") return;
      store.leave(id);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [id]);

  return (
    <NodeToolbar isVisible position={Position.Bottom} offset={PANEL_OFFSET}>
      <motion.div
        initial={{ opacity: 0, y: -6, scale: 0.985 }}
        animate={{ opacity: paneBusy ? 0.35 : 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        style={{ translate: `${shift}px 0`, pointerEvents: paneBusy ? "none" : undefined }}
      >
        <TrimPanel
          width={width}
          thumbs={source.status === "ready" ? source.thumbs : []}
          loadError={source.status === "error" ? source.message : undefined}
          onConfirm={() => void confirm()}
          onClose={close}
        />
      </motion.div>
    </NodeToolbar>
  );
}
