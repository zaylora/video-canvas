import { formatElapsed, type PendingPhase } from "@/utils/tasks/node-view";

/** 产物种类，决定「即将完成」时说正在保存什么 */
type MediaKind = "image" | "video" | "audio";

/** 各种素材在「即将完成」时的说法 */
const SAVING_TEXT: Record<MediaKind, string> = {
  image: "正在保存图片",
  video: "正在保存视频",
  audio: "正在保存音频",
};

/**
 * 还没结束的任务在占位上显示的文案，画布节点和首页结果格子共用，改文案只改这里。
 * @param phase 展示阶段（见 derivePendingPhase）
 * @param mediaType 产物种类
 * @returns 标题、说明（可能没有）和进度（未知为 null）
 */
export function pendingCopy(
  phase: PendingPhase,
  mediaType: MediaKind,
): { title: string; detail: string; progress: number | null } {
  switch (phase.phase) {
    case "queued":
      return { title: "排队中", detail: "前面的任务处理完后会自动开始", progress: null };
    case "running":
      return {
        title: "生成中",
        detail:
          phase.elapsedMs === null
            ? "正在同步任务状态…"
            : `已耗时 ${formatElapsed(phase.elapsedMs)}`,
        progress: phase.progress,
      };
    case "finalizing":
      return { title: "即将完成", detail: SAVING_TEXT[mediaType], progress: null };
  }
}
