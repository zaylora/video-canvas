import { useCallback, useState, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { NodeToolbar, Position, useStore } from "@xyflow/react";
import { toast } from "sonner";

import { addToLibrary } from "@/api/asset-library";
import { FolderPickerDialog } from "@/components/assets/folder-picker-dialog";
import { NodeActionBar } from "@/components/canvas/node-action-bar";
import { OutpaintOverlay } from "@/components/canvas/outpaint-overlay";
import { FrameOverlay } from "@/components/canvas/frame-overlay";
import { TrimOverlay } from "@/components/canvas/trim-overlay";
import { openDirector } from "@/components/director/open-director";
import {
  PANEL_OFFSET,
  useMarqueeSelecting,
  useNodeDragging,
  usePaneBusy,
  usePanelPlacement,
} from "@/components/canvas/hooks/use-panel-placement";
import { NODE_TOOLBAR, type ToolbarTail } from "@/constants/node-toolbar";
import { useFrameNodes } from "@/hooks/use-frame-nodes";
import { useRemoteModels } from "@/hooks/use-models";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useFramePickStore } from "@/store/frame-pick";
import { useOutpaintStore } from "@/store/outpaint";
import { useTrimStore } from "@/store/trim";
import type { CanvasNodeData } from "@/types";
import { downloadMedia, downloadName } from "@/utils/canvas/download";
import { addNodeToLibrary, canAddToLibrary } from "@/utils/canvas/library-entry";
import { toolbarGate } from "@/utils/canvas/node-toolbar-state";
import { outpaintModels } from "@/utils/canvas/outpaint-node";
import { supportsTrim, UNSUPPORTED_TRIM_MESSAGE } from "@/utils/canvas/trim-engine";

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
 * 空节点没有可加工的素材，不出功能区；上传的素材不是模型生成的，不出生成面板；导演台不调模型，也不出生成面板。
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
  const previewNode = useNodePreview();
  const { width, shift } = usePanelPlacement(id);
  const paneBusy = usePaneBusy();
  const nodeDragging = useNodeDragging(id);
  const zoom = useStore((state) => state.transform[2]);
  const outpainting = useOutpaintStore((state) => state.nodeId === id);
  const enterOutpaint = useOutpaintStore((state) => state.enter);
  const trimming = useTrimStore((state) => state.nodeId === id);
  const enterTrim = useTrimStore((state) => state.enter);
  const framePicking = useFramePickStore((state) => state.nodeId === id);
  const enterFramePick = useFramePickStore((state) => state.enter);
  const { captureQuick } = useFrameNodes();
  /** 「加入资产库」的文件夹选择器是否打开 */
  const [pickingFolder, setPickingFolder] = useState(false);
  const imageModels = useRemoteModels("image", data.kind === "image");

  const config = NODE_TOOLBAR[data.kind];
  const gate = toolbarGate(data);
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
  /** 点「视频修剪」：浏览器不支持 WebCodecs、别的视频正在编码时都不进，说明原因 */
  const startTrim = useCallback(() => {
    if (!supportsTrim()) {
      toast(UNSUPPORTED_TRIM_MESSAGE);
      return;
    }
    // 和选帧互斥：进修剪先退出选帧（暂存的帧丢弃）
    useFramePickStore.getState().leave();
    if (!enterTrim(id)) toast("另一段视频正在编码，请先等它完成");
  }, [enterTrim, id]);
  /** 点「截取帧 → 自定义」：不支持 WebCodecs、别的视频正在修剪编码时都不进，说明原因 */
  const startFramePick = useCallback(() => {
    if (!supportsTrim()) {
      toast(UNSUPPORTED_TRIM_MESSAGE);
      return;
    }
    if (!enterFramePick(id)) toast("另一段视频正在编码，请先等它完成");
  }, [enterFramePick, id]);
  const onAction =
    data.kind === "image"
      ? { outpaint: startOutpaint }
      : data.kind === "video"
        ? {
            trim: startTrim,
            "frame-first": () => void captureQuick(id, "first"),
            "frame-last": () => void captureQuick(id, "last"),
            "frame-custom": startFramePick,
          }
        : data.kind === "director"
          ? {
              "director-open": () => openDirector(id, data),
              "director-export": () => openDirector(id, data, { exportOnOpen: true }),
            }
          : undefined;

  const { src, text, label, fileName } = data;
  const onTail: Partial<Record<ToolbarTail, () => void>> = {
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
    // 只有素材已存到服务端的图片、视频、音频节点才有这个按钮，不限来源
    addToLibrary: canAddToLibrary(data) ? () => setPickingFolder(true) : undefined,
  };
  /** 选好文件夹后加入资产库；失败（请求层已弹提示）抛出，让选择器保持打开 */
  const confirmAddToLibrary = async (folderId: string | null) => {
    await addNodeToLibrary(data, folderId, { addToLibrary });
    toast.success("已加入资产库");
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

  // 修剪模式：修剪面板取代平时的功能区和生成面板
  if (trimming) return <TrimOverlay id={id} data={data} />;

  // 选帧模式：选帧面板取代平时的功能区和生成面板
  if (framePicking) return <FrameOverlay id={id} data={data} />;

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
              onAction={onAction}
              onTail={onTail}
            />
          </motion.div>
        </NodeToolbar>
      )}
      {/* 选择器自己用 Portal 渲染，放在功能区外面，功能区淡出、卸载时不会带走它 */}
      <FolderPickerDialog
        open={pickingFolder}
        onOpenChange={setPickingFolder}
        title="加入资产库"
        confirmLabel="加入"
        onConfirm={confirmAddToLibrary}
      />
      {/* 扩图出来的节点提示词是固定的，参考图只有合成图，没有可调的东西，不摆生成面板 */}
      {!data.uploaded && !data.outpaintFrom && data.kind !== "director" && (
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
