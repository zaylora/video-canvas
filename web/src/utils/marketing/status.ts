import type { ProjectSummary, ScriptBar, StepRun, StepRunStatus } from "@/api/marketing/type";

/** 列表卡状态行 */
export interface ProjectStatusLine {
  /** 文字 */
  text: string;
  /** 语气：busy 转圈、running 蓝色转圈、bad 红字、normal 普通 */
  tone: "busy" | "running" | "bad" | "normal";
}

/**
 * 「我的营销片」卡片的状态行：写脚本中 → 出片中 → 写脚本失败 → N 条成片 / N 份脚本待出片。
 * @param item 列表项
 */
export function projectStatusLine(item: ProjectSummary): ProjectStatusLine {
  if (item.scripting) return { text: "写脚本中", tone: "busy" };
  if (item.rendering > 0) return { text: "出片中", tone: "running" };
  if (item.lastRoundFailed) return { text: "写脚本失败", tone: "bad" };
  const { done, draft, failed } = item.scriptCounts;
  if (done > 0) return { text: `${done} 条成片`, tone: "normal" };
  return { text: `${draft + failed} 份脚本待出片`, tone: "normal" };
}

/** 状态条每一格的颜色类：完成绿、出片中蓝、失败红、草稿灰 */
export const BAR_CLASS: Record<ScriptBar, string> = {
  done: "bg-status-success",
  rendering: "bg-status-running",
  failed: "bg-destructive",
  draft: "bg-chrome-hover",
};

/** 环节进度条小标签的状态文字 */
export const STEP_STATUS_LABEL: Record<StepRunStatus, string> = {
  queued: "等待",
  running: "进行中",
  succeeded: "完成",
  failed: "失败",
  skipped: "跳过",
  canceled: "已取消",
  reused: "沿用",
};

/** 环节运行记录按顺序分成两层：第一层事实卡 ∥ 拆解，第二层跟拍 ∥ 创意（没跑的环节不出现） */
export function splitStepLevels(steps: readonly StepRun[]): [StepRun[], StepRun[]] {
  const pick = (keys: string[]) =>
    keys.map((key) => steps.find((s) => s.step === key)).filter((s): s is StepRun => !!s);
  return [pick(["facts", "reverse"]), pick(["follow", "create"])];
}

/** 某个环节最新的运行记录 */
export const stepRunOf = (steps: readonly StepRun[], step: StepRun["step"]): StepRun | undefined =>
  steps.find((item) => item.step === step);
