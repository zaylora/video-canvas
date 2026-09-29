import type { TaskView } from "@/api/generation-task/type";
import { isTerminalStatus, taskKey } from "./status";

export type TaskToastPlan = {
  id: string;
  tone: "success" | "error";
  title: string;
  description?: string;
  href?: string;
};

const KIND_LABEL: Record<string, string> = {
  video: "视频",
  image: "图片",
  audio: "音频",
};

/** 从当前地址里取出正在看的画布 id；不在画布页返回 null */
export function currentCanvasIdFromPath(pathname: string): string | null {
  const match = /^\/canvas\/([^/]+)/.exec(pathname);
  return match ? decodeURIComponent(match[1]) : null;
}

/**
 * 这次快照变更要不要提醒用户：
 * 必须是刚进入终态（此前没见过，或此前还是进行中），
 * 对账时拉回来的陈年终态任务（source=reconcile 且本地没有记录）不提醒。
 */
export function isFreshTerminalTransition(
  prev: TaskView | undefined,
  next: TaskView,
  source: "live" | "reconcile",
): boolean {
  if (!isTerminalStatus(next.status)) return false;
  if (prev) return !isTerminalStatus(prev.status);
  return source === "live";
}

/**
 * 画布外的完成提示：用户正看着这张画布时节点自己会变，不弹；
 * 在列表页或别的画布时弹「《画布名》中的视频已生成 / 生成失败」，点击跳转。
 * 主动取消不提醒。
 */
export function planTaskToast(
  view: TaskView,
  currentCanvasId: string | null,
  canvasTitle: string | undefined,
): TaskToastPlan | null {
  const canvasId = view.canvas_id === null || view.canvas_id === undefined ? null : String(view.canvas_id);
  if (canvasId !== null && canvasId === currentCanvasId) return null;

  const kind = KIND_LABEL[view.kind] ?? "内容";
  const name = `《${canvasTitle || "画布"}》`;
  const href = canvasId !== null ? `/canvas/${canvasId}` : undefined;
  const id = `task:${taskKey(view.id)}`;

  switch (view.status) {
    case "succeeded":
      return { id, tone: "success", title: `${name}中的${kind}已生成`, href };
    case "failed":
    case "expired":
      return {
        id,
        tone: "error",
        title: `${name}中的${kind}${view.status === "expired" ? "生成超时" : "生成失败"}`,
        description: `${view.error_message ? `${view.error_message}，` : ""}积分已退回`,
        href,
      };
    default:
      return null;
  }
}
