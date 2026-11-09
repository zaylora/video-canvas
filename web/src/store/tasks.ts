import { create } from "zustand";

import type { TaskView } from "@/api/generation-task/type";
import { mergeTask, mergeTasks, type TaskMap } from "@/utils/tasks/merge";
import { isActiveStatus, matchesTaskId } from "@/utils/tasks/status";

export type TaskApplied = { view: TaskView; prev?: TaskView };

type TasksState = {
  /** taskId -> 最新快照；只在 version 更大时更新 */
  tasks: TaskMap;
  /** 合并一份快照，返回是否真的生效（旧版本、重复推送返回 null） */
  upsert: (view: TaskView) => TaskApplied | null;
  /** 一次合并多份，返回生效的那些 */
  upsertMany: (views: TaskView[]) => TaskApplied[];
};

/**
 * 任务快照库：WebSocket 推送和 HTTP 对账都往这里写，
 * 画布里的节点订阅它来渲染进度、并在终态时回填。
 */
export const useTasksStore = create<TasksState>((set, get) => ({
  tasks: {},

  upsert: (view) => {
    const result = mergeTask(get().tasks, view);
    if (!result.applied) return null;
    set({ tasks: result.tasks });
    return { view, prev: result.prev };
  },

  upsertMany: (views) => {
    const result = mergeTasks(get().tasks, views);
    if (result.applied.length > 0) set({ tasks: result.tasks });
    return result.applied;
  },
}));

/** 本画布里还没结束（pending / queued / running / finalizing）的任务数 */
export const countCanvasRunningTasks = (tasks: TaskMap, canvasId: string | null | undefined) =>
  canvasId
    ? Object.values(tasks).filter((t) => t.canvas_id === canvasId && isActiveStatus(t.status))
        .length
    : 0;

/** 订阅本画布进行中的任务数；返回 number，数值不变时不触发重渲染 */
export const useCanvasRunningTaskCount = (canvasId: string | null | undefined) =>
  useTasksStore((state) => countCanvasRunningTasks(state.tasks, canvasId));

/** 通过十进制 ID 或编码任务编号订阅快照，任务库仍只保存一份规范 ID 的记录。 */
export const useTask = (taskId: string | undefined) =>
  useTasksStore((state) =>
    taskId
      ? (state.tasks[taskId] ??
        Object.values(state.tasks).find((task) => matchesTaskId(task, taskId)))
      : undefined,
  );
