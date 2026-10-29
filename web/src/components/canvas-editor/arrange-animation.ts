import type { XYPosition } from "@xyflow/react";

import type { FlowNode } from "@/types";

/** 整理布局的过渡时长，毫秒 */
export const ARRANGE_DURATION = 260;

type FlowOps = {
  getNodes: () => FlowNode[];
  setNodes: (update: (nodes: FlowNode[]) => FlowNode[]) => void;
};

/**
 * 把一批节点用缓动移到目标位置（targets 里是各节点自己坐标系下的 position）。
 * 过渡期间标成 dragging，撤销栈只在落定时记一步；落定那一帧顺手调 onDone，
 * 它里面的 setNodes 和最后一帧落在同一次渲染里，所以收尾（比如贴合组框）也算同一步。
 */
export function animatePositions(
  { getNodes, setNodes }: FlowOps,
  targets: ReadonlyMap<string, XYPosition>,
  onDone?: (nodes: FlowNode[]) => FlowNode[],
) {
  const from = new Map(
    getNodes()
      .filter((node) => targets.has(node.id))
      .map((node) => [node.id, node.position]),
  );
  const start = performance.now();
  const step = (now: number) => {
    const t = Math.min(1, (now - start) / ARRANGE_DURATION);
    const eased = 1 - Math.pow(1 - t, 3);
    setNodes((nodes) => {
      const moved = nodes.map((node) => {
        const to = targets.get(node.id);
        const origin = from.get(node.id);
        if (!to || !origin) return node;
        return {
          ...node,
          dragging: t < 1,
          position: {
            x: origin.x + (to.x - origin.x) * eased,
            y: origin.y + (to.y - origin.y) * eased,
          },
        } as FlowNode;
      });
      return t >= 1 && onDone ? onDone(moved) : moved;
    });
    if (t < 1) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}
