import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNodeData } from "@/types";

/** 视频节点正文该长什么样 */
export type VideoNodeView =
  | { phase: "idle" }
  | { phase: "queued" }
  | { phase: "running"; elapsedMs: number | null; progress: number | null }
  | { phase: "finalizing" }
  | { phase: "done"; src: string }
  | { phase: "failed"; message: string; refunded: boolean };

/** 平台给的进度按 0–100 取整并夹住；不是有限数字就当没有 */
export function normalizeProgress(progress: number | null | undefined): number | null {
  if (typeof progress !== "number" || !Number.isFinite(progress)) return null;
  return Math.min(100, Math.max(0, Math.round(progress)));
}

/**
 * 节点数据 + 任务快照 -> 视频节点的展示状态（设计 6.3 节的表）。
 * 「提交中」是点击到 202 之间的本地瞬态，不在这里，由生成按钮的 loading 表达。
 */
export function deriveVideoNodeView(
  data: Pick<CanvasNodeData, "status" | "src" | "error" | "taskId">,
  task: TaskView | undefined,
  now: number,
): VideoNodeView {
  switch (data.status) {
    case "running": {
      if (!task) return { phase: "running", elapsedMs: null, progress: null };
      switch (task.status) {
        case "pending":
        case "queued":
          return { phase: "queued" };
        case "running": {
          const started = Date.parse(task.created_at);
          return {
            phase: "running",
            elapsedMs: Number.isNaN(started) ? null : Math.max(0, now - started),
            progress: normalizeProgress(task.progress),
          };
        }
        // 转存中；终态但节点还没被回填的一瞬间，也显示「即将完成」
        default:
          return { phase: "finalizing" };
      }
    }
    case "error":
      return {
        phase: "failed",
        message: data.error || "生成失败",
        // 有 taskId 说明是任务失败，后端已经把冻结的积分退回
        refunded: !!data.taskId,
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
