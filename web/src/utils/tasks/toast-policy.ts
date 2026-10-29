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
  text: "文本",
};

/** 失败提示的说明：原因 + 退款 + 任务编号（用户把它报给运维，就能在日志里搜到失败原因） */
function failureDescription(view: TaskView): string {
  const reason = view.error_message ? `${view.error_message}，` : "";
  const ref = view.task_ref ? ` · 任务 ID：${view.task_ref}` : "";
  return `${reason}积分已退回${ref}`;
}

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

/** 首页生成的对话任务的 node_id 前缀（rec:{记录id}:{格子序号}），它们没有画布 */
const CONVERSATION_NODE_PREFIX = "rec:";

/** 短剧片段任务的 node_id 前缀：seg:{片段id} 是片段视频 */
const SEGMENT_NODE_PREFIXES = ["seg:"] as const;

/**
 * 画布外的完成提示：用户正看着这张画布时节点自己会变，不弹；
 * 在列表页或别的画布时弹「《画布名》中的视频已生成 / 生成失败」，点击跳转。
 * 首页生成的对话任务没有画布：正在对话页时格子自己会变，不弹；在别的页面弹不带画布名的说法，没有跳转。
 * 主动取消不提醒。
 * @param view 任务快照
 * @param currentCanvasId 正在看的画布 id，不在画布页为 null
 * @param canvasTitle 任务所属画布的标题
 * @param pathname 当前地址（路径加查询串，如 /drama/1a2b?area=studio），判断是不是正在看对话页或片场
 */
export function planTaskToast(
  view: TaskView,
  currentCanvasId: string | null,
  canvasTitle: string | undefined,
  pathname = "",
): TaskToastPlan | null {
  if (view.node_id.startsWith(CONVERSATION_NODE_PREFIX)) {
    return planConversationToast(view, pathname);
  }
  if (SEGMENT_NODE_PREFIXES.some((prefix) => view.node_id.startsWith(prefix))) {
    return planSegmentToast(view, pathname);
  }
  const canvasId =
    view.canvas_id === null || view.canvas_id === undefined ? null : String(view.canvas_id);
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
        description: failureDescription(view),
        href,
      };
    default:
      return null;
  }
}

/** 对话任务的提示：不带画布名，没有跳转目标 */
function planConversationToast(view: TaskView, pathname: string): TaskToastPlan | null {
  if (pathname.startsWith("/conversations/")) return null;
  const kind = KIND_LABEL[view.kind] ?? "内容";
  const id = `task:${taskKey(view.id)}`;
  switch (view.status) {
    case "succeeded":
      return { id, tone: "success", title: `${kind}已生成` };
    case "failed":
    case "expired":
      return {
        id,
        tone: "error",
        title: `${kind}${view.status === "expired" ? "生成超时" : "生成失败"}`,
        description: failureDescription(view),
      };
    default:
      return null;
  }
}

/**
 * 正在看片场：工作台的 ?area=studio（片场已并入工作台），或旧的 /drama/:id/ep/:no（会被重定向，
 * 重定向前的一瞬间也算）。查询参数顺序不固定，所以按参数解析，不用正则硬匹配。
 */
function inStudio(location: string): boolean {
  if (/^\/drama\/[^/?]+\/ep\//.test(location)) return true;
  const match = /^\/drama\/[^/?]+\?(.*)$/.exec(location);
  return !!match && new URLSearchParams(match[1]).get("area") === "studio";
}

/** 片段任务的提示：正在片场时片段自己会变，不弹；短剧画布不是普通画布，不带画布名也不跳转 */
function planSegmentToast(view: TaskView, pathname: string): TaskToastPlan | null {
  if (inStudio(pathname)) return null;
  const name = "片段视频";
  const id = `task:${taskKey(view.id)}`;
  switch (view.status) {
    case "succeeded":
      return { id, tone: "success", title: `${name}已生成` };
    case "failed":
    case "expired":
      return {
        id,
        tone: "error",
        title: `${name}${view.status === "expired" ? "生成超时" : "生成失败"}`,
        description: failureDescription(view),
      };
    default:
      return null;
  }
}
