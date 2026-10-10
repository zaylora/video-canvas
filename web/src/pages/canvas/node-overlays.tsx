import { useCallback, useEffect, type ReactNode } from "react";
import { motion } from "motion/react";
import { NodeToolbar, Position, useReactFlow, useStore } from "@xyflow/react";
import { toast } from "sonner";

import { FramePickerPanel } from "@/components/canvas/frame-picker-panel";
import { NodeActionBar } from "@/components/canvas/node-action-bar";
import {
  PANEL_OFFSET,
  useNodeDragging,
  usePaneBusy,
  usePanelPlacement,
} from "@/components/canvas/hooks/use-panel-placement";
import { NODE_TOOLBAR } from "@/constants/node-toolbar";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { useFrameNodes } from "@/hooks/use-frame-nodes";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useFramePickStore } from "@/store/frame-pick";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { downloadMedia, downloadName } from "@/utils/canvas/download";
import { toolbarGate } from "@/utils/canvas/node-toolbar-state";
import { activeOutputIdOf, readOutputs, selectOutput } from "@/utils/canvas/outputs";

import { useNodePreview } from "./preview-context";

/** 标题行（13px 字加 8px 间距）在画布 100% 时离卡片顶边的高度，随缩放一起缩 */
const TITLE_ROW_HEIGHT = 28;
/** 功能区离标题行的距离，不随缩放：100% 时合起来是 44，和原来的历史浮条一致 */
const TOOLBAR_GAP = 16;
/** 功能区往上浮的距离：出现和退出都是这么多（设计稿 6.16 动效表） */
const TOOLBAR_RISE = 6;

/**
 * 选中节点时浮出的两块：上方的功能区（设计稿 6.16，历史也在里面）、下方的生成面板。
 * 空节点没有可加工的素材，不出功能区；上传的素材不是模型生成的，不出生成面板。
 * 只在选中时挂载，所以摆法计算（订阅视口变化）只花在一个节点上。
 * 面板始终在节点下方、功能区始终在上方，位置不随视口里的空间变化；拖动画布或节点时两者淡下去。
 * 退出动画靠外层的 AnimatePresence：两块都带 exit，取消选中时一起淡出。
 */
export function NodeOverlays({
  id,
  data,
  children,
}: {
  id: string;
  data: CanvasNodeData;
  /** 生成面板，拿到算好的宽度 */
  children: (width: number) => ReactNode;
}) {
  const { updateNodeData } = useReactFlow<CanvasNode, CanvasEdge>();
  const history = useCanvasHistoryContext();
  const previewNode = useNodePreview();
  const { captureQuick, placeFrames } = useFrameNodes();
  // 选帧状态在 store 里：画幅里的预览层也要知道自己是不是在选帧
  const picking = useFramePickStore((state) => state.nodeId === id);
  const beginPick = useFramePickStore((state) => state.begin);
  const endPick = useFramePickStore((state) => state.end);
  // 选中了别的节点、功能区被收走时，这个节点退出选帧（只会清自己的，不碰别人的）
  useEffect(() => () => endPick(id), [endPick, id]);
  const { width, shift } = usePanelPlacement(id);
  const paneBusy = usePaneBusy();
  const nodeDragging = useNodeDragging(id);
  const zoom = useStore((state) => state.transform[2]);

  const config = NODE_TOOLBAR[data.kind];
  const gate = toolbarGate(data);
  const outputs = readOutputs(data);

  const selectVersion = useCallback(
    (outputId: string) => {
      // 生成中不切：正文显示的是任务进度，切了也看不到，还会把状态改乱
      if (data.status === "running") {
        toast("生成中不能切换版本");
        return;
      }
      // 失败后点当前那一版，等于把它找回来，照样放行
      if (outputId === activeOutputIdOf(data) && data.status === "done") return;
      history?.record();
      updateNodeData(id, (node) => {
        const patch = selectOutput(node.data, outputId);
        return patch ? { ...patch, status: "done", error: null } : {};
      });
    },
    [data, history, id, updateNodeData],
  );

  const { src, text, label, fileName } = data;
  const onTail = {
    // 灯箱只认图片和视频，文本、音频的放大先当占位
    zoom:
      previewNode && (data.kind === "image" || data.kind === "video")
        ? () => previewNode(id)
        : undefined,
    download: src ? () => void downloadMedia(src, downloadName(label, src, fileName)) : undefined,
    copy: text
      ? () =>
          void navigator.clipboard.writeText(text).then(
            () => toast.success("已复制文本"),
            () => toast.error("复制失败，请手动选中复制"),
          )
      : undefined,
  };

  // 视频的「截取帧」：首帧、尾帧一键截；自定义在节点下方换上选帧面板
  const onAction =
    data.kind === "video"
      ? {
          "frame-first": () => void captureQuick(id, "first"),
          "frame-last": () => void captureQuick(id, "last"),
          "frame-custom": () => beginPick(id),
        }
      : undefined;
  const confirmPick = (files: File[]) => {
    placeFrames(id, files);
    endPick(id);
  };

  const fade = {
    opacity: paneBusy ? 0.35 : 1,
    pointerEvents: paneBusy ? ("none" as const) : undefined,
  };
  const exit = { opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } };

  // 拖着这个节点时两块都收起，松手后重新浮出来；选帧中不收，不然暂存的帧会跟着丢
  if (nodeDragging && !picking) return null;

  return (
    <>
      {gate.visible && !picking && (
        <NodeToolbar
          isVisible
          position={Position.Top}
          offset={TITLE_ROW_HEIGHT * zoom + TOOLBAR_GAP}
        >
          <motion.div
            initial={{ opacity: 0, y: TOOLBAR_RISE }}
            animate={{ opacity: fade.opacity, y: 0 }}
            exit={{ ...exit, y: TOOLBAR_RISE }}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            style={{ pointerEvents: fade.pointerEvents }}
          >
            <NodeActionBar
              config={config}
              label={label}
              gate={gate}
              history={
                config.tail.includes("history")
                  ? {
                      nodeId: id,
                      outputs,
                      activeId: activeOutputIdOf(data),
                      onSelect: selectVersion,
                    }
                  : undefined
              }
              onAction={onAction}
              onTail={onTail}
            />
          </motion.div>
        </NodeToolbar>
      )}
      {(picking || !data.uploaded) && (
        <NodeToolbar isVisible position={Position.Bottom} offset={PANEL_OFFSET}>
          <motion.div
            initial={{ opacity: 0, y: -6, scale: 0.985 }}
            animate={{ opacity: fade.opacity, y: 0, scale: 1 }}
            exit={exit}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            style={{ translate: `${shift}px 0`, pointerEvents: fade.pointerEvents }}
          >
            {picking && src ? (
              <FramePickerPanel
                src={src}
                label={label}
                width={width}
                onConfirm={confirmPick}
                onClose={() => endPick(id)}
              />
            ) : (
              children(width)
            )}
          </motion.div>
        </NodeToolbar>
      )}
    </>
  );
}
