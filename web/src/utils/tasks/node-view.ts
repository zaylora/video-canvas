import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNodeData } from "@/types";
import { isUploading } from "@/utils/canvas/upload-state";
import { isTerminalStatus } from "@/utils/tasks/status";

/** 视频节点正文该长什么样 */
export type VideoNodeView =
  | { phase: "idle" }
  | { phase: "queued" }
  | { phase: "running"; elapsedMs: number | null; progress: number | null }
  | { phase: "finalizing" }
  | { phase: "uploading"; progress: number }
  | { phase: "done"; src: string }
  | { phase: "failed"; message: string; refunded: boolean; taskRef: string | null };

/** 平台给的进度按 0–100 取整并夹住；不是有限数字就当没有 */
export function normalizeProgress(progress: number | null | undefined): number | null {
  if (typeof progress !== "number" || !Number.isFinite(progress)) return null;
  return Math.min(100, Math.max(0, Math.round(progress)));
}

/** 任务还没结束时的展示阶段：排队中 / 生成中 / 即将完成（转存） */
export type PendingPhase =
  | { phase: "queued" }
  | { phase: "running"; elapsedMs: number | null; progress: number | null }
  | { phase: "finalizing" };

/** 通用任务占位状态；成功产物交给调用方渲染，尚未回填时显示保存阶段。 */
export type GenerationStatusView = PendingPhase | Extract<VideoNodeView, { phase: "failed" }>;

/** 页面已有的任务摘要，在完整任务快照尚未同步时提供状态和失败原因。 */
export type TaskStatusFallback = Pick<TaskView, "status" | "error_message">;

/** 任务快照与页面摘要转成统一状态；终态摘要不能被任务库里迟到的进行中快照覆盖。 */
export function deriveTaskStatus(
  task: TaskView | undefined,
  now: number,
  fallback?: TaskStatusFallback,
): GenerationStatusView {
  const snapshot =
    fallback && isTerminalStatus(fallback.status) && task && !isTerminalStatus(task.status)
      ? undefined
      : task;
  const status = snapshot?.status ?? fallback?.status;
  if (status && isTerminalStatus(status) && status !== "succeeded") {
    return {
      phase: "failed",
      message:
        snapshot?.error_message ||
        fallback?.error_message ||
        (status === "canceled" ? "已取消" : status === "expired" ? "生成超时" : "生成失败"),
      refunded: snapshot?.charged_credits === 0,
      taskRef: snapshot?.task_ref ?? null,
    };
  }
  if (snapshot) return derivePendingPhase(snapshot, now);
  if (status === "finalizing" || status === "succeeded") return { phase: "finalizing" };
  return { phase: "running", elapsedMs: null, progress: null };
}

/**
 * 任务快照 -> 还没结束时的展示阶段，画布节点和首页对话的结果格子共用这一套说法（设计 6.3 节的表）。
 * 排队中 = 还没调用上游（pending 且没有提交时间）；生成中 = 已经在调用上游，耗时从提交那一刻起算：
 * 异步接口是已被上游受理（queued / running），同步接口（如生图）则是请求还没返回、状态仍是 pending 但已记下提交时间；
 * 其余（转存中，以及终态但还没被回填的一瞬间）= 即将完成。
 * @param task 任务快照
 * @param now 当前时间（毫秒），算已耗时用
 * @returns 展示阶段
 */
export function derivePendingPhase(task: TaskView, now: number): PendingPhase {
  // 排队中：pending 且没有提交时间，任务还没提交给上游，在等我们这边的执行名额 / 渠道并发限制
  if (task.status === "pending" && !task.submitted_at) return { phase: "queued" };
  // 生成中：已经调用上游（含 pending 但已记下提交时间的同步请求）。上游内部是在排队还是在出图，用户不需要分清
  if (task.status === "pending" || task.status === "queued" || task.status === "running") {
    const started = Date.parse(task.submitted_at ?? task.created_at);
    return {
      phase: "running",
      elapsedMs: Number.isNaN(started) ? null : Math.max(0, now - started),
      progress: normalizeProgress(task.progress),
    };
  }
  return { phase: "finalizing" };
}

/**
 * 节点数据 + 任务快照 -> 视频节点的展示状态（设计 6.3 节的表）。
 * 「提交中」是点击到 202 之间的本地瞬态，不在这里，由生成按钮的 loading 表达。
 */
export function deriveVideoNodeView(
  data: Pick<
    CanvasNodeData,
    "status" | "src" | "error" | "errorTaskRef" | "taskId" | "uploadProgress"
  >,
  task: TaskView | undefined,
  now: number,
): VideoNodeView {
  switch (data.status) {
    case "running":
      // 本地文件在上传：展示上传进度，不是生成任务
      if (isUploading(data)) return { phase: "uploading", progress: data.uploadProgress ?? 0 };
      if (!task) return { phase: "running", elapsedMs: null, progress: null };
      return derivePendingPhase(task, now);
    case "error":
      return {
        phase: "failed",
        message: data.error || "生成失败",
        // 有 taskId 说明是任务失败，后端已经把冻结的积分退回
        refunded: !!data.taskId,
        // 任务编号用来到后端日志里定位：节点里存的是失败那一刻记下的，重开画布后也在；
        // 还没来得及存时看任务快照。没有任务的本地错误（如上传失败）为 null
        taskRef: data.taskId ? (data.errorTaskRef ?? task?.task_ref ?? null) : null,
      };
    // 上传进来的素材（status 可能还是 idle，src 已经有了）和生成成功的一样直接摆出来
    default:
      return data.src ? { phase: "done", src: data.src } : { phase: "idle" };
  }
}

/** 89000 -> 「1分29秒」，不到一分钟只显示秒 */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return minutes > 0 ? `${minutes}分${String(seconds).padStart(2, "0")}秒` : `${seconds}秒`;
}
