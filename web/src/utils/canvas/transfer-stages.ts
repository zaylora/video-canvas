/** 进度弹窗的流程种类：导出是画布 → 文件，导入是文件 → 画布 */
export type TransferKind = "export" | "import";

/** 一个阶段相对「当前阶段」的状态 */
export type StageStatus = "done" | "now" | "todo";

/**
 * 两个流程的阶段文案。
 * 后端是一次请求，前端拿不到逐步进度，所以这些只是按时间轮播的说明，
 * 告诉用户「在做什么」，不代表后端真实进度，也不显示百分比。
 */
export const TRANSFER_STAGES: Record<TransferKind, readonly string[]> = {
  export: ["保存最新内容", "整理素材", "生成文件"],
  import: ["检查文件", "复制素材", "创建新画布"],
};

/** 每个阶段停留的时间（毫秒） */
export const STAGE_INTERVAL_MS = 1600;

/**
 * 弹窗打开了这么久，应该停在第几个阶段。
 * 到最后一个阶段就停住：请求没返回之前不假装「已完成」。
 * @param elapsedMs 弹窗已打开的毫秒数；负数和不是数字按 0 算
 * @param total 阶段总数
 */
export function stageIndexAt(elapsedMs: number, total: number): number {
  if (total <= 0) return 0;
  const safe = Number.isNaN(elapsedMs) || elapsedMs < 0 ? 0 : elapsedMs;
  return Math.min(Math.floor(safe / STAGE_INTERVAL_MS), total - 1);
}

/**
 * 某个阶段相对当前阶段的状态：当前之前是 done，当前是 now，之后是 todo。
 * @param index 阶段下标
 * @param current 当前阶段下标
 */
export function stageStatus(index: number, current: number): StageStatus {
  if (index < current) return "done";
  return index === current ? "now" : "todo";
}
