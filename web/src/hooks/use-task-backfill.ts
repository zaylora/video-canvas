import { useEffect, useRef, useState } from "react";
import { useReactFlow } from "@xyflow/react";

import { reconcileTaskIds } from "@/utils/ws/task-events";
import { useTasksStore } from "@/store/tasks";
import type { CanvasNode } from "@/types";
import { canvasNotice, petTakesNotices, type NoticeOptions } from "@/utils/pet/notice";
import {
  NodeIdAccumulator,
  buildResultNotices,
  isFreshTerminal,
  type ResultNotice,
} from "@/utils/pet/result-notice";
import { applyBackfill } from "@/utils/tasks/backfill";
import { collectRunningTaskIds, markTasksGone } from "@/utils/tasks/reconcile";
import { isActiveStatus, taskKey } from "@/utils/tasks/status";

/** 点击结果气泡定位节点时的视口动画 */
const LOCATE_DURATION = 400;

/** 桌宠气泡的呈现：成功庆祝、失败和批次失败播失败动作，都归「生成结果」类 */
const bubbleStyle = (n: ResultNotice): NoticeOptions => {
  switch (n.level) {
    case "success":
      return { source: "result", priority: "result", mood: "jumping", ttl: 5000 };
    case "warning":
      return { source: "result", priority: "error", mood: "failed", ttl: 6000 };
    case "error":
      return { source: "result" };
    default:
      return {};
  }
};

/**
 * 任务结果回填画布：
 * - 任务 store 里出现终态、且节点的 taskId 与之一致时，写回节点（走现有自动保存）；
 *   已经是这个结果的节点跳过，节点被删了自然忽略；
 * - 打开画布时，把 status=running 的节点的 taskId 收集起来按 id 对账，
 *   这样关页面后第二天再打开，结果还在；
 * - 回填产生的事件交给 canvasNotice：桌宠接管时进气泡（成功按画布和种类合并成「3 张图好了」，
 *   点击用 fitView 定位到这些节点），否则退回 toast。只对本次会话里新变成终态的任务冒成功和失败气泡，
 *   打开画布时已经结束的不冒，刷新页面不会重复庆祝。
 */
export function useTaskBackfill(
  nodes: CanvasNode[],
  setNodes: React.Dispatch<React.SetStateAction<CanvasNode[]>>,
  canvasId: string | null = null,
) {
  const tasks = useTasksStore((state) => state.tasks);
  const { fitView } = useReactFlow();
  const fitViewRef = useRef(fitView);
  useEffect(() => {
    fitViewRef.current = fitView;
  });
  const [accumulator] = useState(() => new NodeIdAccumulator());
  /** 打开画布的时间，和本次会话里见过处于进行中的任务一起，判断终态是不是新的 */
  const openedAt = useRef(0);
  const seenActive = useRef(new Set<string>());
  useEffect(() => {
    openedAt.current = Date.now();
  }, []);

  // 记下本次会话里见过在进行中的任务；放在回填 effect 前面，同一次更新里先记再判断
  useEffect(() => {
    for (const view of Object.values(tasks)) {
      if (isActiveStatus(view.status)) seenActive.current.add(taskKey(view.id));
    }
  }, [tasks]);

  // 每次节点或任务快照变化都过一遍；没有可回填的就什么都不做
  useEffect(() => {
    const { events } = applyBackfill(nodes, tasks);
    if (events.length === 0) return;
    setNodes((prev) => applyBackfill(prev, tasks).nodes);
    const petActive = petTakesNotices();
    const notices = buildResultNotices(events, canvasId, (view) =>
      isFreshTerminal(view, seenActive.current, openedAt.current),
    );
    for (const notice of notices) {
      // 成功和整体失败原来没有 toast，桌宠没接管时不新增
      if (notice.petOnly && !petActive) continue;
      const ids = notice.key
        ? accumulator.add(notice.key, notice.nodeIds, Date.now())
        : new Set(notice.nodeIds);
      canvasNotice(notice.level, notice.text, {
        id: notice.id,
        key: notice.key,
        ...bubbleStyle(notice),
        onClick:
          notice.level === "info"
            ? undefined
            : () =>
                void fitViewRef.current({
                  nodes: [...ids].map((id) => ({ id })),
                  duration: LOCATE_DURATION,
                  padding: 0.3,
                  maxZoom: 1,
                }),
      });
    }
  }, [nodes, tasks, setNodes, canvasId, accumulator]);

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
