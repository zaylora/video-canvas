import { create } from "zustand";

import type { TaskView } from "@/api/generation-task/type";
import { mergeTask, mergeTasks, type TaskMap } from "@/utils/tasks/merge";

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

/** 订阅单个任务；taskId 为空或还没有快照时返回 undefined */
export const useTask = (taskId: string | undefined) =>
  useTasksStore((state) => (taskId ? state.tasks[taskId] : undefined));
