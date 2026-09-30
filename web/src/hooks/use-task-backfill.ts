import { useEffect, useRef } from "react";

import { reconcileTaskIds } from "@/utils/ws/task-events";
import { toast } from "sonner";
import { useTasksStore } from "@/store/tasks";
import type { CanvasNode } from "@/types";
import { applyBackfill } from "@/utils/tasks/backfill";
import { collectRunningTaskIds } from "@/utils/tasks/reconcile";

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
        prev.map((node) =>
          node.data.status === "running" &&
          node.data.taskId &&
          gone.has(node.data.taskId) &&
          !useTasksStore.getState().tasks[node.data.taskId]
            ? {
                ...node,
                data: {
                  ...node.data,
                  status: "error",
                  taskId: undefined,
                  error: "任务状态已失效，请重新生成",
                },
              }
            : node,
        ),
      );
    });
    return () => {
      active = false;
    };
  }, [setNodes]);
}
