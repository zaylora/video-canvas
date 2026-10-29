import type { AgentEventDto, AgentWatchBatchDto, AgentWatchTaskDto } from "@/api/agent/type";

import type { AgentSessionState } from "./session-state";

/** 监听条里的一个任务 */
export type WatchTaskView = {
  /** 生成任务 ID */
  taskId: string;
  /** 它绑定的画布节点，点击后定位 */
  nodeId: string;
  /** 节点标题 */
  label: string;
  /** 生成中 / 成功 / 失败 */
  statusText: "生成中" | "成功" | "失败";
};

/** 监听条要显示的内容：多批合并后的进度 */
export type WatchSummary = {
  /** 任务总数（各批相加） */
  total: number;
  /** 已结束的任务数 */
  settled: number;
  /** 结束了但没成功的（失败、超时、取消、不存在） */
  failed: number;
  /** 有批已全部结束、在等当前运行结束后交给 Agent */
  queued: boolean;
  /** 条上的一句话 */
  text: string;
  /** 逐个任务（按批的顺序） */
  tasks: WatchTaskView[];
};

/** 还在进行的批：等待中，或已全部结束在排队等 Agent 继续 */
const ACTIVE_WATCH_STATES: readonly AgentWatchBatchDto["state"][] = ["waiting", "deliverable"];

/** 会话里进行中的后台等待，按批的登记顺序（事件先后） */
export function activeWatches(state: AgentSessionState): AgentWatchBatchDto[] {
  return Object.values(state.watches).filter((b) => ACTIVE_WATCH_STATES.includes(b.state));
}

const taskView = (t: AgentWatchTaskDto): WatchTaskView => ({
  taskId: t.task_id,
  nodeId: t.node_id,
  label: t.label,
  statusText: t.status === "pending" ? "生成中" : t.task_status === "succeeded" ? "成功" : "失败",
});

/**
 * 把进行中的批合并成监听条的内容；没有批返回 null（条不显示）。
 * @param batches 进行中的批
 */
export function summarizeWatches(batches: readonly AgentWatchBatchDto[]): WatchSummary | null {
  if (batches.length === 0) return null;
  const tasks = batches.flatMap((b) => b.tasks.map(taskView));
  const total = batches.reduce((n, b) => n + b.total, 0);
  const settled = batches.reduce((n, b) => n + b.settled, 0);
  const failed = tasks.filter((t) => t.statusText === "失败").length;
  const queued = batches.some((b) => b.state === "deliverable");
  const text = queued
    ? "已全部结束 · 等当前运行结束后 Agent 继续"
    : `后台等待生成 · ${settled}/${total} 已结束`;
  return { total, settled, failed, queued, text, tasks };
}

/**
 * 画布标签页标题的前缀：有待处理的审批或提问时「⏸ 待批准 · 」；否则有后台等待在进行时「⏳ k/n · 」；都没有为空串。
 * 已全部结束、只是在排队的等待不显示进度（它不再需要用户盯着）
 * @param pending 有没有等你决定的审批或提问
 * @param summary 监听条的内容
 */
export function watchTitlePrefix(pending: boolean, summary: WatchSummary | null): string {
  if (pending) return "⏸ 待批准 · ";
  if (summary && !summary.queued) return `⏳ ${summary.settled}/${summary.total} · `;
  return "";
}

/** 要弹出的系统通知 */
export type AgentNotice = {
  /** 通知标题 */
  title: string;
};

/**
 * 一条实时到达的事件要不要弹系统通知：页面在后台时，① 出现待批准的生成 ② Agent 因生成全部结束而新开一轮（插进当前轮不通知，
 * 用户正在聊，不需要提醒）。其余事件、页面在前台都不通知。
 * @param event 实时到达的事件
 * @param ctx 画布名（没起名按「未命名画布」）和页面是否在后台
 * @returns 通知内容，不通知为 null
 */
export function notificationFor(
  event: AgentEventDto,
  ctx: { canvasTitle: string; hidden: boolean },
): AgentNotice | null {
  if (!ctx.hidden) return null;
  const name = ctx.canvasTitle.trim() || "未命名画布";
  const data = event.data ?? {};
  if (event.type === "approval.created" && data.kind === "generate" && data.status === "pending") {
    return { title: `${name}：Agent 需要你批准一批生成` };
  }
  if (event.type === "tasks.settled" && data.steer === false) {
    return { title: `${name}：生成已完成，Agent 正在验收` };
  }
  return null;
}

/**
 * 第一次批准生成时请求系统通知权限：已经问过（允许或拒绝）、浏览器不支持时什么也不做，不在页面加载时弹
 */
export function ensureNotificationPermission(): void {
  if (typeof Notification === "undefined" || Notification.permission !== "default") return;
  void Notification.requestPermission().catch(() => undefined);
}

/**
 * 弹一条系统通知；没有权限或浏览器不支持时什么也不做。点通知聚焦回这个窗口
 * @param notice 通知内容
 */
export function showNotice(notice: AgentNotice): void {
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
  try {
    const n = new Notification(notice.title, { tag: "agent-notice" });
    n.onclick = () => {
      window.focus();
      n.close();
    };
  } catch {
    // 个别环境（如移动端）构造通知会抛错：通知只是提醒，不影响使用
  }
}
