import { useCallback, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { NodeToolbar, Position, useReactFlow, useStore } from "@xyflow/react";
import { toast } from "sonner";

import { NodeActionBar } from "@/components/canvas/node-action-bar";
import { OutpaintOverlay } from "@/components/canvas/outpaint-overlay";
import {
  PANEL_OFFSET,
  useMarqueeSelecting,
  useNodeDragging,
  usePaneBusy,
  usePanelPlacement,
} from "@/components/canvas/hooks/use-panel-placement";
import { NODE_TOOLBAR } from "@/constants/node-toolbar";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { useRemoteModels } from "@/hooks/use-models";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useOutpaintStore } from "@/store/outpaint";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { downloadMedia, downloadName } from "@/utils/canvas/download";
import { toolbarGate } from "@/utils/canvas/node-toolbar-state";
import { activeOutputIdOf, readOutputs, selectOutput } from "@/utils/canvas/outputs";
import { outpaintModels } from "@/utils/canvas/outpaint-node";

import { useNodePreview } from "./preview-context";

/** 标题行（13px 字加 8px 间距）在画布 100% 时离卡片顶边的高度，随缩放一起缩 */
const TITLE_ROW_HEIGHT = 28;
/** 功能区离标题行的距离，不随缩放：100% 时合起来是 44，和原来的历史浮条一致 */
const TOOLBAR_GAP = 16;
/** 功能区往上浮的距离：出现和退出都是这么多（设计稿 6.16 动效表） */
const TOOLBAR_RISE = 6;

/**
 * 节点浮层的出场容器：单选时靠 AnimatePresence 给淡出，多选和框选中则整个卸载、不留退出动画。
 * 框选时节点会先后被单独选中，浮层刚进场就被「多选」打断；退出动画一旦没走完，
 * AnimatePresence 就不会摘掉它，浮层会残留在选区工具条旁边（刷新才消失）。
 * 多选本来就由选区工具条接管；框选没松手时选区还在变，只框中一个也不该先弹出半透明的浮层，
 * 这两种情况都不需要淡出，直接卸载最稳，松手后若是单选再正常进场。
 * @param props.multiSelected 画布当前是否选中了多个节点
 * @param props.children 浮层本身，带 key，出现与否由调用方按选中状态决定
 */
export function NodeOverlaysPresence({
  multiSelected,
  children,
}: {
  multiSelected: boolean;
  children: ReactNode;
}) {
  const marquee = useMarqueeSelecting();
  if (multiSelected || marquee) return null;
  return <AnimatePresence>{children}</AnimatePresence>;
}

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
  const { width, shift } = usePanelPlacement(id);
  const paneBusy = usePaneBusy();
  const nodeDragging = useNodeDragging(id);
  const zoom = useStore((state) => state.transform[2]);
  const outpainting = useOutpaintStore((state) => state.nodeId === id);
  const enterOutpaint = useOutpaintStore((state) => state.enter);
  const imageModels = useRemoteModels("image", data.kind === "image");

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

  /** 点「扩图」：只进入扩图模式、摆出框和面板，不生成任何图片；没有能做扩图的模型就不进 */
  const startOutpaint = useCallback(() => {
    if (imageModels.status !== "ready") {
      toast("正在加载模型清单，稍后再试");
      return;
    }
    if (outpaintModels(imageModels.models).length === 0) {
      toast("暂无支持图生图的模型");
      return;
    }
    enterOutpaint(id);
  }, [enterOutpaint, id, imageModels.models, imageModels.status]);
  const onAction = data.kind === "image" ? { outpaint: startOutpaint } : undefined;

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

  const fade = {
    opacity: paneBusy ? 0.35 : 1,
    pointerEvents: paneBusy ? ("none" as const) : undefined,
  };
  const exit = { opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } };

  // 拖着这个节点时两块都收起，松手后重新浮出来
  if (nodeDragging) return null;

  // 扩图模式：框 + 面板取代平时的功能区和生成面板
  if (outpainting) return <OutpaintOverlay id={id} data={data} />;

  return (
    <>
      {gate.visible && (
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
      {/* 扩图出来的节点提示词是固定的，参考图只有合成图，没有可调的东西，不摆生成面板 */}
      {!data.uploaded && !data.outpaintFrom && (
        <NodeToolbar isVisible position={Position.Bottom} offset={PANEL_OFFSET}>
          <motion.div
            initial={{ opacity: 0, y: -6, scale: 0.985 }}
            animate={{ opacity: fade.opacity, y: 0, scale: 1 }}
            exit={exit}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            style={{ translate: `${shift}px 0`, pointerEvents: fade.pointerEvents }}
          >
            {children(width)}
          </motion.div>
        </NodeToolbar>
      )}
    </>
  );
}
