import { useEffect, useRef } from "react";

import { reconcileTaskIds } from "@/utils/ws/task-events";
import { toast } from "sonner";
import { useTasksStore } from "@/store/tasks";
import type { CanvasNode } from "@/types";
import { applyBackfill, failedBatchMembers } from "@/utils/tasks/backfill";
import { collectRunningTaskIds, markTasksGone } from "@/utils/tasks/reconcile";

/**
 * 任务结果回填画布：
 * - 任务 store 里出现终态、且节点的 taskId 与之一致时，写回节点（走现有自动保存）；
 *   已经是这个结果的节点跳过，节点被删了自然忽略；
 * - 打开画布时，把 status=running 的节点的 taskId 收集起来按 id 对账，
 *   这样关页面后第二天再打开，结果还在。
 */
export function useTaskBackfill(
  nodes: CanvasNode[],
  setNodes: React.Dispatch<React.SetStateAction<CanvasNode[]>>,
) {
  const tasks = useTasksStore((state) => state.tasks);

  // 每次节点或任务快照变化都过一遍；没有可回填的就什么都不做
  useEffect(() => {
    const { events } = applyBackfill(nodes, tasks);
    if (events.length === 0) return;
    setNodes((prev) => applyBackfill(prev, tasks).nodes);
    for (const event of events) {
      if (event.view.status === "canceled") {
        toast.info("已取消生成，积分已退回", { id: `canceled:${event.view.id}` });
      }
    }
    // 一批任务里有失败的、节点却已经有结果：节点不会变成失败态，用一条提示说清楚
    for (const [nodeId, messages] of Object.entries(failedBatchMembers(events))) {
      toast.warning(`有 ${messages.length} 张生成失败，积分已退回：${messages[0]}`, {
        id: `batch-failed:${nodeId}:${events.find((e) => e.nodeId === nodeId)?.view.id}`,
      });
    }
  }, [nodes, tasks, setNodes]);

  // 打开画布只对账一次
  const initialNodesRef = useRef(nodes);
  useEffect(() => {
    let active = true;
    const ids = collectRunningTaskIds(initialNodesRef.current, useTasksStore.getState().tasks);
    if (ids.length === 0) return;
    void reconcileTaskIds(ids).then((missing) => {
      if (!active || !missing || missing.length === 0) return;
      // 服务端确认没有这些任务（被清理或不属于当前用户）：别让节点永远转圈
      const gone = new Set(missing);
      setNodes((prev) =>
        prev.map((node) => {
          // 本地已经有快照的任务由回填处理，不算「没了」
          const stillGone = new Set([...gone].filter((id) => !useTasksStore.getState().tasks[id]));
          const patch = markTasksGone(node.data, stillGone);
          return patch ? { ...node, data: { ...node.data, ...patch } } : node;
        }),
      );
    });
    return () => {
      active = false;
    };
  }, [setNodes]);
}
