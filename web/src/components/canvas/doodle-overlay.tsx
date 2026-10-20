import { useCallback, useEffect, useRef, useState } from "react";
import { motion } from "motion/react";
import { NodeToolbar, Position, useReactFlow, useStore, type ReactFlowState } from "@xyflow/react";
import { toast } from "sonner";

import { DoodleStage } from "@/components/canvas/doodle-stage";
import { DoodleToolbar } from "@/components/canvas/doodle-toolbar";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { useImageSize } from "@/hooks/use-image-size";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useDoodleStore } from "@/store/doodle";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import {
  DEFAULT_DOODLE_COLOR,
  DOODLE_WIDTH_DEFAULT,
  doodleCanvasSize,
} from "@/utils/canvas/doodle";
import type { DoodleEngine, DoodleTool } from "@/utils/canvas/doodle-engine";
import { renderDoodle } from "@/utils/canvas/doodle-export";
import { buildDoodleNode, DOODLE_FILE_NAME } from "@/utils/canvas/doodle-node";
import { isGroupNode } from "@/utils/canvas/group";
import { loadSourceImage } from "@/utils/canvas/outpaint-compose";
import { uploadRunner } from "@/utils/canvas/upload-runner";

/** 标题行的高度（画布坐标）：工具栏要浮在标题行上方，和平时的功能区同一个位置 */
const TITLE_ROW_HEIGHT = 28;

/** 工具栏离标题行的间隙（屏幕像素） */
const TOOLBAR_GAP = 16;

/** 没有任何标注就点保存时的提示 */
const EMPTY_TEXT = "还没有标注内容";

/**
 * 涂鸦模式（设计稿 5）：图片上叠一块 Fabric 画布，顶部是标注工具栏。
 * 点保存才建新的图片节点（上传型，源节点右侧，从源节点连线）；× 关闭、取消选中、多选都直接退出，
 * 没保存的笔迹丢弃，不弹确认。取代这个节点平时的功能区和生成面板。
 * @param id 被涂鸦的图片节点 id
 * @param data 节点数据，src 是要标注的图
 */
export function DoodleOverlay({ id, data }: { id: string; data: CanvasNodeData }) {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const history = useCanvasHistoryContext();
  const leave = useDoodleStore((state) => state.leave);
  const zoom = useStore((state: ReactFlowState) => state.transform[2]);
  const natural = useImageSize(data.src);

  const [tool, setTool] = useState<DoodleTool>("pen");
  const [color, setColor] = useState<string>(DEFAULT_DOODLE_COLOR);
  const [width, setWidth] = useState(DOODLE_WIDTH_DEFAULT);
  const [ready, setReady] = useState(false);
  /** 撤销 / 重做 / 有无内容的快照：引擎里的栈变了就刷新，按钮据此置灰 */
  const [stack, setStack] = useState({ canUndo: false, canRedo: false });
  const engineRef = useRef<DoodleEngine | null>(null);
  /** 已经点过保存：防止 leave 生效前的连点建出两个节点 */
  const savedRef = useRef(false);

  const syncStack = useCallback(() => {
    const engine = engineRef.current;
    setStack({ canUndo: !!engine?.canUndo(), canRedo: !!engine?.canRedo() });
  }, []);

  const close = useCallback(() => leave(id), [id, leave]);

  // Esc 退出；正在编辑文字时 Esc 是结束编辑（Fabric 自己处理），不退出涂鸦
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if ((event.target as HTMLElement | null)?.dataset?.fabric === "textarea") return;
      leave(id);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [id, leave]);

  /** 把先建出来的节点撤掉（读图 / 导出失败） */
  const discard = useCallback(
    (nodeId: string) => {
      uploadRunner.cancel(nodeId);
      setNodes((nodes) => nodes.filter((node) => node.id !== nodeId));
      setEdges((edges) => edges.filter((edge) => edge.source !== nodeId && edge.target !== nodeId));
    },
    [setEdges, setNodes],
  );

  const save = () => {
    const engine = engineRef.current;
    if (!engine || savedRef.current || !data.src || !natural) return;
    // 先结束正在编辑的文字，它才会算进内容里
    engine.commit();
    if (!engine.hasContent()) {
      toast(EMPTY_TEXT);
      return;
    }
    const source = getNode(id);
    if (!source || isGroupNode(source)) return;
    savedRef.current = true;
    // 退出涂鸦会卸载画布，所以标注层要在退出前先导出成独立的画布
    const layer = engine.exportLayer();
    const src = data.src;
    const size = doodleCanvasSize(natural);
    const { node, edge } = buildDoodleNode({
      source,
      nodes: getNodes(),
      aspect: natural.w / natural.h,
    });
    // 建节点、连线算一步撤销；读图、上传写回的字段撤销不倒回
    history?.record();
    setNodes((nodes) => [...nodes, node]);
    setEdges((edges) => [...edges, edge]);
    // 图还没导出好：先占位，免得被误判成被中断的上传而标红
    uploadRunner.hold(node.id);
    leave(id);
    void (async () => {
      try {
        const bitmap = await loadSourceImage(src);
        let blob: Blob;
        try {
          blob = await renderDoodle(bitmap, layer, size);
        } finally {
          bitmap.close();
        }
        // 导出期间节点被用户删掉（占位被清）就不传了
        if (!uploadRunner.isActive(node.id)) return;
        const file = new File([blob], DOODLE_FILE_NAME, { type: "image/png" });
        const outcome = await uploadRunner.start(node.id, file, (nodeId, patch) =>
          updateNodeData(nodeId, patch),
        );
        if (outcome === "failed") toast.error("涂鸦图上传失败，可以在节点上重试");
      } catch (error) {
        discard(node.id);
        toast.error(error instanceof Error ? error.message : "保存失败，请稍后重试");
      }
    })();
  };

  return (
    <>
      <DoodleStage
        nodeId={id}
        natural={natural}
        tool={tool}
        color={color}
        width={width}
        ready={ready}
        engineRef={engineRef}
        onChange={syncStack}
        onReady={setReady}
        onFail={close}
      />
      <NodeToolbar isVisible position={Position.Top} offset={TITLE_ROW_HEIGHT * zoom + TOOLBAR_GAP}>
        <motion.div
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: DURATION.base, ease: EASE_OUT }}
        >
          <DoodleToolbar
            tool={tool}
            onToolChange={setTool}
            color={color}
            onColorChange={setColor}
            width={width}
            onWidthChange={setWidth}
            canUndo={stack.canUndo}
            canRedo={stack.canRedo}
            onUndo={() => engineRef.current?.undo()}
            onRedo={() => engineRef.current?.redo()}
            onSave={save}
            onClose={close}
            onInteract={() => engineRef.current?.commit()}
            saveDisabled={!ready}
          />
        </motion.div>
      </NodeToolbar>
    </>
  );
}
