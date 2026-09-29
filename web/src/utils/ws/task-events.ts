import type { TaskView } from "@/api/generation-task/type";
import {
  getActiveGenerationTasks,
  getGenerationTasksByIds,
} from "@/api/generation-task";
import { useCreditsStore } from "@/store/credits";
import { useTasksStore } from "@/store/tasks";
import { toast } from "sonner";
import { getCanvasTitle } from "@/utils/canvas/title-cache";
import { findStaleActiveIds } from "@/utils/tasks/reconcile";
import {
  currentCanvasIdFromPath,
  isFreshTerminalTransition,
  planTaskToast,
} from "@/utils/tasks/toast-policy";

/** 快照来源：WebSocket 实时推送，或 HTTP 对账拉回来的 */
export type TaskSource = "live" | "reconcile";

/**
 * 所有任务快照的唯一入口：合并进 store；
 * 刚进入终态的顺便刷新积分，并在画布外弹完成提示。
 */
export function handleTaskView(view: TaskView, source: TaskSource) {
  const applied = useTasksStore.getState().upsert(view);
  if (!applied) return;
  if (!isFreshTerminalTransition(applied.prev, applied.view, source)) return;

  void useCreditsStore.getState().refresh();
  const plan = planTaskToast(
    applied.view,
    currentCanvasIdFromPath(window.location.pathname),
    getCanvasTitle(applied.view.canvas_id),
  );
  if (!plan) return;
  const { href } = plan;
  toast[plan.tone](plan.title, {
    id: plan.id,
    description: plan.description,
    // 动态引入 router，避免 router -> ws-runtime -> 本文件 的循环依赖
    action: href
      ? { label: "点击查看", onClick: () => void import("@/router").then(({ router }) => router.navigate(href)) }
      : undefined,
  });
}

/**
 * 全量对账：拉当前用户所有进行中的任务写进 store，
 * 再把「本地还当它在跑、服务端进行中列表里却没有」的按 id 补查一遍，拿到断线期间错过的终态。
 */
export async function reconcileActiveTasks() {
  try {
    const active = await getActiveGenerationTasks();
    for (const view of active) handleTaskView(view, "reconcile");
    const stale = findStaleActiveIds(useTasksStore.getState().tasks, active);
    if (stale.length > 0) {
      for (const view of await getGenerationTasksByIds(stale)) {
        handleTaskView(view, "reconcile");
      }
    }
  } catch {
    // 对账失败不打扰用户，下次重连 / 回到前台会再来一次
  }
}

/**
 * 按 id 对账（打开画布时用）；这些都是很久以前提交的，不当作刚完成的事件提醒。
 * 返回服务端没有返回的 id（任务不存在）；请求失败返回 null，调用方保持现状。
 */
export async function reconcileTaskIds(ids: string[]): Promise<string[] | null> {
  if (ids.length === 0) return [];
  try {
    const views = await getGenerationTasksByIds(ids);
    for (const view of views) handleTaskView(view, "reconcile");
    const got = new Set(views.map((view) => String(view.id)));
    return ids.filter((id) => !got.has(id));
  } catch {
    // 节点保持「生成中」，之后的推送或重连对账会补上
    return null;
  }
}
