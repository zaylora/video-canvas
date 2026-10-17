import { useEffect, useRef } from "react";
import { useReactFlow } from "@xyflow/react";

import type { CanvasTool } from "@/components/canvas";
import type { CanvasEdge, FlowNode } from "@/types";
import { cloneGroup, selectionEdges } from "@/utils/canvas/clone-group";
import { absolutePosition, isGroupNode } from "@/utils/canvas/group";
import {
  clipboardTextOf,
  decidePaste,
  readClipboard,
  type ClipboardContent,
} from "@/utils/canvas/paste";

import { VIEWPORT_DURATION } from "./chrome/view-controls";

/** 复制到剪贴板的节点：跨画布也能粘（同一个标签页里） */
let clipboard: { nodes: FlowNode[]; edges: CanvasEdge[] } | null = null;

/** 复制节点时写进系统剪贴板的文字：粘贴时靠它认出剪贴板里还是不是刚复制的那批节点 */
let clipboardText = "";

/** 刚按下复制、等着 copy 事件取走的那段文字；copy 事件一过就清空 */
let pendingCopyText: string | null = null;

/** 焦点在能打字的地方、或者在弹窗菜单里时，按键是给那边的 */
function isBusyTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  if (["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName)) return true;
  return !!target.closest("[role=dialog], [role=menu], [role=listbox]");
}

type FlowOps = Pick<
  ReturnType<typeof useReactFlow<FlowNode, CanvasEdge>>,
  "getNodes" | "getEdges" | "setNodes" | "setEdges"
>;

/** 选区的读取与插入：快捷键和多选工具条共用 */
function selectionOps({ getNodes, getEdges, setNodes, setEdges }: FlowOps) {
  const insert = (group: { nodes: FlowNode[]; edges: CanvasEdge[] }) => {
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
    // 选中了组就连成员一起算：成员没被点选，但复制 / 粘贴要带上
    const all = getNodes();
    const groupIds = new Set(
      all.filter((node) => node.selected && isGroupNode(node)).map((node) => node.id),
    );
    const nodes = all.filter(
      (node) => node.selected || (node.parentId && groupIds.has(node.parentId)),
    );
    const ids = new Set(nodes.map((node) => node.id));
    return { nodes, edges: selectionEdges(ids, getEdges()) };
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
  group,
  paste,
}: {
  undo: () => void;
  redo: () => void;
  setTool: (tool: CanvasTool) => void;
  openShortcuts: () => void;
  /** 立即保存，不等停手 */
  save: () => void;
  /** 组的快捷键：⌘G 打组、⇧⌘G 解组、Enter / F2 给选中的组改名 */
  group: { group: () => void; ungroup: () => void; rename: () => boolean };
  /** 粘贴来自剪贴板的外部内容（文件或文字）；at 是指针处的画布坐标，指针不在画布上为 null */
  paste: (content: ClipboardContent, at: { x: number; y: number } | null) => void;
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
  } = useReactFlow<FlowNode, CanvasEdge>();
  const pointer = useRef<{ x: number; y: number } | null>(null);

  useEffect(() => {
    const onPointerMove = (event: PointerEvent) => {
      const overPane = (event.target as Element | null)?.closest?.(
        ".react-flow__pane, .react-flow__node",
      );
      pointer.current = overPane ? { x: event.clientX, y: event.clientY } : null;
    };

    const { insert, selection } = selectionOps({ getNodes, getEdges, setNodes, setEdges });

    /** 粘回复制的节点：有指针就粘到指针处（以选区左上角对齐），没有就在原位错开一点 */
    const pasteNodes = (source: { nodes: FlowNode[]; edges: CanvasEdge[] }) => {
      // 对齐的是最外层元素的左上角：组和不在这次复制范围内的组里的节点，按绝对位置算
      const copiedIds = new Set(source.nodes.map((node) => node.id));
      const outer = source.nodes.filter((node) => !node.parentId || !copiedIds.has(node.parentId));
      const everything = getNodes();
      const left = Math.min(...outer.map((node) => absolutePosition(node, everything).x));
      const top = Math.min(...outer.map((node) => absolutePosition(node, everything).y));
      const at = pointer.current ? screenToFlowPosition(pointer.current) : null;
      const offset = at ? { x: at.x - left, y: at.y - top } : { x: 48, y: 48 };
      insert(cloneGroup(source, offset, getNodes()));
      if (!at) clipboard = cloneGroup(source, { x: 48, y: 48 });
    };

    /** 复制节点时顺手把节点文字写进系统剪贴板，粘贴时靠它分清是粘节点还是粘外部内容 */
    const onCopy = (event: ClipboardEvent) => {
      if (pendingCopyText === null || !event.clipboardData) return;
      event.clipboardData.setData("text/plain", pendingCopyText);
      event.preventDefault();
      clipboardText = pendingCopyText;
      pendingCopyText = null;
    };

    /** 粘贴：粘回复制的节点，或把剪贴板里的文件、文字交给调用方落成节点；输入框里的粘贴不管 */
    const onPaste = (event: ClipboardEvent) => {
      if (isBusyTarget(event.target) || !event.clipboardData) return;
      const content = readClipboard(event.clipboardData);
      const decision = decidePaste(content, clipboardText, !!clipboard?.nodes.length);
      if (decision === "ignore") return;
      event.preventDefault();
      if (decision === "nodes" && clipboard) pasteNodes(clipboard);
      else paste(content, pointer.current ? screenToFlowPosition(pointer.current) : null);
    };

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
        if (picked.nodes.length) {
          clipboard = structuredClone(picked);
          // 紧接着的 copy 事件把这段文字写进系统剪贴板；事件没来就作废，免得盖掉之后别处的复制
          pendingCopyText = clipboardTextOf(
            picked.nodes.flatMap((node) => (isGroupNode(node) ? [] : [node.data])),
          );
          window.setTimeout(() => (pendingCopyText = null), 0);
        }
        return;
      }
      if (mod && key === "g") {
        event.preventDefault();
        if (event.shiftKey) group.ungroup();
        else group.group();
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
      if ((event.key === "Enter" || event.key === "F2") && group.rename()) {
        event.preventDefault();
        return;
      }
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
    window.addEventListener("copy", onCopy);
    window.addEventListener("paste", onPaste);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("copy", onCopy);
      window.removeEventListener("paste", onPaste);
    };
  }, [
    fitView,
    getEdges,
    getNodes,
    openShortcuts,
    group,
    paste,
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
