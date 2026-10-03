import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";

import type { CanvasEdge, CanvasNode } from "@/types";
import {
  HISTORY_LIMIT,
  contentKey,
  restoreNodes,
  structureKey,
  type HistorySnapshot,
} from "@/utils/canvas/history";

/** 连续改提示词、参数时，停手多久才算一步 */
const TYPING_SETTLE_MS = 800;

type Keyed = HistorySnapshot & { structure: string; content: string };

const keyed = (nodes: CanvasNode[], edges: CanvasEdge[]): Keyed => {
  const snapshot = { nodes, edges };
  return { ...snapshot, structure: structureKey(snapshot), content: contentKey(snapshot) };
};

/**
 * 画布的撤销 / 重做：盯着节点和连线，变了就把变之前的样子压栈，不需要各处改动自己报备。
 * - 增删节点、连线、拖完节点：立刻算一步；拖动过程中不记
 * - 改标题、提示词、参数：停手 800ms 才算一步，连续打字不会一个字一步
 * - 任务回填（状态、产物）不算一步，撤销时也不倒回去，见 restoreNodes
 * - 切历史版本这类只改 activeOutputId 的操作，调用方先 record() 再改
 * - 提示词里 @ 素材顺手连的线，调用方先 absorbTyping()，和正在攒的打字算同一步
 * 409 冲突时画布整张重挂，撤销栈随之清空。
 */
export function useCanvasHistory({
  nodes,
  edges,
  setNodes,
  setEdges,
}: {
  nodes: CanvasNode[];
  edges: CanvasEdge[];
  setNodes: React.Dispatch<React.SetStateAction<CanvasNode[]>>;
  setEdges: React.Dispatch<React.SetStateAction<CanvasEdge[]>>;
}) {
  const past = useRef<HistorySnapshot[]>([]);
  const future = useRef<HistorySnapshot[]>([]);
  const latest = useRef<Keyed | null>(null);
  const typingBase = useRef<HistorySnapshot | null>(null);
  const typingTimer = useRef<number | null>(null);
  const restoring = useRef(false);
  const absorbNext = useRef(false);
  const [depth, setDepth] = useState({ past: 0, future: 0, typing: false });

  const sync = useCallback(
    () =>
      setDepth({
        past: past.current.length,
        future: future.current.length,
        typing: typingBase.current !== null,
      }),
    [],
  );

  const push = useCallback(
    (snapshot: HistorySnapshot) => {
      past.current.push({ nodes: snapshot.nodes, edges: snapshot.edges });
      if (past.current.length > HISTORY_LIMIT) past.current.shift();
      future.current = [];
      sync();
    },
    [sync],
  );

  /** 把攒着的打字那一步落进栈里 */
  const settleTyping = useCallback(() => {
    absorbNext.current = false;
    if (typingTimer.current !== null) window.clearTimeout(typingTimer.current);
    typingTimer.current = null;
    if (typingBase.current) push(typingBase.current);
    typingBase.current = null;
    sync();
  }, [push, sync]);

  useEffect(() => {
    const current = keyed(nodes, edges);
    const previous = latest.current;
    if (!previous || restoring.current) {
      restoring.current = false;
      latest.current = current;
      return;
    }
    if (nodes.some((node) => node.dragging)) return;
    if (current.structure !== previous.structure) {
      // 被打过招呼的这次连线并进攒着的打字：撤销一次回到开始打字之前，而不是停在打了一半的 @ 上
      const base = absorbNext.current ? typingBase.current : null;
      if (base) typingBase.current = null;
      settleTyping();
      push(base ?? previous);
    } else if (current.content !== previous.content) {
      if (!typingBase.current) {
        typingBase.current = previous;
        sync();
      }
      if (typingTimer.current !== null) window.clearTimeout(typingTimer.current);
      typingTimer.current = window.setTimeout(settleTyping, TYPING_SETTLE_MS);
    }
    latest.current = current;
  }, [edges, nodes, push, settleTyping, sync]);

  useEffect(
    () => () => {
      if (typingTimer.current !== null) window.clearTimeout(typingTimer.current);
    },
    [],
  );

  const travel = useCallback(
    (from: React.RefObject<HistorySnapshot[]>, to: React.RefObject<HistorySnapshot[]>) => {
      settleTyping();
      const present = latest.current;
      if (!present) return false;
      // 和眼下一模一样的快照（系统自动修正留下的）直接跳过，免得按一下撤销没反应
      let target = from.current.pop();
      while (target) {
        const key = keyed(target.nodes, target.edges);
        const sameVersions = target.nodes.every(
          (node, index) => node.data.activeOutputId === present.nodes[index]?.data.activeOutputId,
        );
        if (key.structure !== present.structure || key.content !== present.content || !sameVersions)
          break;
        target = from.current.pop();
      }
      if (!target) {
        sync();
        return false;
      }
      to.current.push({ nodes: present.nodes, edges: present.edges });
      restoring.current = true;
      setNodes((current) => restoreNodes(target.nodes, current));
      setEdges(target.edges);
      sync();
      return true;
    },
    [setEdges, setNodes, settleTyping, sync],
  );

  const undo = useCallback(() => travel(past, future), [travel]);
  const redo = useCallback(() => travel(future, past), [travel]);

  /** 下一个改动不会被自动发现（比如只换了当前版本）时，先手动记一步 */
  const record = useCallback(() => {
    settleTyping();
    if (latest.current) push(latest.current);
  }, [push, settleTyping]);

  /** 紧接着的那次结构改动（比如 @ 素材时自动连的线）和正在攒的打字合成一步 */
  const absorbTyping = useCallback(() => {
    absorbNext.current = true;
  }, []);

  return {
    undo,
    redo,
    record,
    absorbTyping,
    canUndo: depth.past > 0 || depth.typing,
    canRedo: depth.future > 0,
  };
}

export type CanvasHistory = ReturnType<typeof useCanvasHistory>;

const CanvasHistoryContext = createContext<CanvasHistory | null>(null);

export const CanvasHistoryProvider = CanvasHistoryContext.Provider;

/** 节点里要手动记一步时用；不在画布里时返回 null */
export const useCanvasHistoryContext = () => useContext(CanvasHistoryContext);
