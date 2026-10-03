import { useEffect, useRef } from "react";
import { useReactFlow } from "@xyflow/react";

import type { CanvasTool } from "@/components/canvas";
import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { copyLabels } from "@/utils/canvas/node-label";

import { VIEWPORT_DURATION } from "./chrome/view-controls";

/** 复制到剪贴板的节点：跨画布也能粘（同一个标签页里） */
let clipboard: { nodes: CanvasNode[]; edges: CanvasEdge[] } | null = null;

/** 焦点在能打字的地方、或者在弹窗菜单里时，按键是给那边的 */
function isBusyTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  if (["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName)) return true;
  return !!target.closest("[role=dialog], [role=menu], [role=listbox]");
}

/** 复制出来的节点不带任务：在跑的任务属于原节点，副本回到「有结果就 done，没有就 idle」 */
function cloneData(data: CanvasNodeData): CanvasNodeData {
  const next = structuredClone(data);
  delete next.taskId;
  if (next.status === "running" || next.status === "error") {
    next.status = next.src ? "done" : "idle";
    next.error = null;
  }
  return next;
}

/**
 * 把一组节点和它们之间的连线复制一份，整体平移 offset，新副本处于选中状态。
 * 给了 onCanvas（画布上眼下的节点）就按「X 副本」「X 副本二」起名，避开已有的名字；
 * 只是挪剪贴板里的位置时不给，名字原样留着。
 */
function cloneGroup(
  source: { nodes: CanvasNode[]; edges: CanvasEdge[] },
  offset: { x: number; y: number },
  onCanvas?: CanvasNode[],
) {
  const ids = new Map(source.nodes.map((node) => [node.id, crypto.randomUUID()]));
  const used = onCanvas?.map((node) => node.data.label) ?? [];
  const nodes = source.nodes.map((node) => {
    const data = cloneData(node.data);
    if (onCanvas) {
      [data.label] = copyLabels(node.data.label, used, 1);
      used.push(data.label);
    }
    return {
      id: ids.get(node.id) as string,
      type: node.type,
      position: { x: node.position.x + offset.x, y: node.position.y + offset.y },
      selected: true,
      data,
    };
  });
  const edges = source.edges.map((edge) => ({
    ...edge,
    ...ANIMATED_EDGE_OPTIONS,
    id: crypto.randomUUID(),
    source: ids.get(edge.source) as string,
    target: ids.get(edge.target) as string,
    selected: false,
  }));
  return { nodes, edges };
}

type FlowOps = Pick<
  ReturnType<typeof useReactFlow<CanvasNode, CanvasEdge>>,
  "getNodes" | "getEdges" | "setNodes" | "setEdges"
>;

/** 选区的读取与插入：快捷键和多选工具条共用 */
function selectionOps({ getNodes, getEdges, setNodes, setEdges }: FlowOps) {
  const insert = (group: { nodes: CanvasNode[]; edges: CanvasEdge[] }) => {
    setNodes((nodes) => [
      ...nodes.map((node) => (node.selected ? { ...node, selected: false } : node)),
      ...group.nodes,
    ]);
    setEdges((edges) => [
      ...edges.map((edge) => (edge.selected ? { ...edge, selected: false } : edge)),
      ...group.edges,
    ]);
  };
  const selection = () => {
    const nodes = getNodes().filter((node) => node.selected);
    const ids = new Set(nodes.map((node) => node.id));
    const edges = getEdges().filter((edge) => ids.has(edge.source) && ids.has(edge.target));
    return { nodes, edges };
  };
  return { insert, selection };
}

/** 原地复制选中的节点（连同它们之间的线），副本错开 32px 并选中 */
export function duplicateSelection(flow: FlowOps) {
  const { insert, selection } = selectionOps(flow);
  const picked = selection();
  if (picked.nodes.length) insert(cloneGroup(picked, { x: 32, y: 32 }, flow.getNodes()));
}

/**
 * 画布快捷键（设计稿 6.5）：撤销重做、复制粘贴、原地复制、工具切换、缩放、聚焦提示词。
 * 删除沿用 xyflow 自带的 Backspace / Delete。
 */
export function useCanvasShortcuts({
  undo,
  redo,
  setTool,
  openShortcuts,
  save,
}: {
  undo: () => void;
  redo: () => void;
  setTool: (tool: CanvasTool) => void;
  openShortcuts: () => void;
  /** 立即保存，不等停手 */
  save: () => void;
}) {
  const {
    getNodes,
    getEdges,
    setNodes,
    setEdges,
    screenToFlowPosition,
    fitView,
    zoomTo,
    zoomIn,
    zoomOut,
  } = useReactFlow<CanvasNode, CanvasEdge>();
  const pointer = useRef<{ x: number; y: number } | null>(null);

  useEffect(() => {
    const onPointerMove = (event: PointerEvent) => {
      const overPane = (event.target as Element | null)?.closest?.(
        ".react-flow__pane, .react-flow__node",
      );
      pointer.current = overPane ? { x: event.clientX, y: event.clientY } : null;
    };

    const { insert, selection } = selectionOps({ getNodes, getEdges, setNodes, setEdges });

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.isComposing) return;
      const mod = event.metaKey || event.ctrlKey;
      const key = event.key.toLowerCase();

      // 保存在输入框里也要生效，同时挡掉浏览器的「保存网页」
      if (mod && key === "s" && !event.shiftKey && !event.altKey) {
        event.preventDefault();
        save();
        return;
      }
      if (isBusyTarget(event.target)) return;

      if (mod && key === "z") {
        event.preventDefault();
        if (event.shiftKey) redo();
        else undo();
        return;
      }
      if (mod && key === "y") {
        event.preventDefault();
        redo();
        return;
      }
      if (mod && key === "c") {
        const picked = selection();
        if (picked.nodes.length) clipboard = structuredClone(picked);
        return;
      }
      if (mod && key === "v") {
        if (!clipboard?.nodes.length) return;
        event.preventDefault();
        // 有指针就粘到指针处（以选区左上角对齐），没有就在原位错开一点
        const left = Math.min(...clipboard.nodes.map((node) => node.position.x));
        const top = Math.min(...clipboard.nodes.map((node) => node.position.y));
        const at = pointer.current ? screenToFlowPosition(pointer.current) : null;
        const offset = at ? { x: at.x - left, y: at.y - top } : { x: 48, y: 48 };
        insert(cloneGroup(clipboard, offset, getNodes()));
        if (!at) clipboard = cloneGroup(clipboard, { x: 48, y: 48 });
        return;
      }
      if (mod && key === "d") {
        event.preventDefault();
        duplicateSelection({ getNodes, getEdges, setNodes, setEdges });
        return;
      }
      if (mod && (key === "=" || key === "+")) {
        event.preventDefault();
        void zoomIn({ duration: VIEWPORT_DURATION });
        return;
      }
      if (mod && key === "-") {
        event.preventDefault();
        void zoomOut({ duration: VIEWPORT_DURATION });
        return;
      }
      if (mod || event.altKey) return;

      if (event.shiftKey && event.code === "Digit1") {
        void fitView({ duration: VIEWPORT_DURATION, padding: 0.2 });
        return;
      }
      if (event.shiftKey && event.code === "Digit0") {
        void zoomTo(1, { duration: VIEWPORT_DURATION });
        return;
      }
      if (event.key === "?") {
        openShortcuts();
        return;
      }
      if (key === "v") setTool("select");
      if (key === "h") setTool("pan");
      if (event.key === "Enter") {
        const prompt = document.querySelector<HTMLElement>("[data-node-prompt]");
        if (prompt) {
          event.preventDefault();
          prompt.focus();
          // 提示词是可编辑的 div（Tiptap），光标挪到末尾
          const selection = window.getSelection();
          selection?.selectAllChildren(prompt);
          selection?.collapseToEnd();
        }
      }
      if (event.key === "Escape") {
        setNodes((nodes) =>
          nodes.map((node) => (node.selected ? { ...node, selected: false } : node)),
        );
      }
    };

    window.addEventListener("pointermove", onPointerMove, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [
    fitView,
    getEdges,
    getNodes,
    openShortcuts,
    redo,
    save,
    screenToFlowPosition,
    setEdges,
    setNodes,
    setTool,
    undo,
    zoomIn,
    zoomOut,
    zoomTo,
  ]);
}
