import type { ProbeIssue, ProbeResult } from "@/api/admin/storage/type";

/** 步骤的展示状态：通过、失败、按配置跳过、因前面失败没执行 */
export type ProbeStepStatus = "ok" | "failed" | "skipped" | "notrun";

/** 展示用的一个测试步骤 */
export type ProbeStepView = {
  /** 步骤序号，从 1 开始 */
  index: number;
  /** 步骤名称 */
  name: string;
  /** 展示状态 */
  status: ProbeStepStatus;
  /** 耗时文本，如 82ms、1.2s；跳过、没执行或没有耗时时为空串 */
  durationText: string;
  /** 失败说明；只有失败步骤且后端给了说明时才有这个字段 */
  issue?: ProbeIssue;
};

/** 毫秒 → 展示文本：不足 1 秒用 ms，否则用秒保留一位 */
const durationText = (ms: number) => {
  if (!(ms > 0)) return "";
  return ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(1)}s`;
};

/**
 * 把探针结果整理成展示用的步骤列表。
 * 后端用 ok / skipped 两个布尔表达四种状态：ok+skipped 是按配置主动跳过，
 * 非 ok+skipped 是前面失败所以没执行，两者页面上要区别开。
 * @param result 探针结果；为空时返回空数组
 * @returns 步骤列表，顺序与后端一致
 */
export function probeSteps(result: ProbeResult | null | undefined): ProbeStepView[] {
  return (result?.steps ?? []).map((step) => {
    if (step.skipped) {
      return {
        index: step.index,
        name: step.name,
        status: step.ok ? "skipped" : "notrun",
        durationText: "",
      };
    }
    const view: ProbeStepView = {
      index: step.index,
      name: step.name,
      status: step.ok ? "ok" : "failed",
      durationText: durationText(step.duration_ms),
    };
    if (!step.ok && step.issue) view.issue = step.issue;
    return view;
  });
}

/**
 * 一句话概括测试结果，给结果区的标题标签用。
 * @param result 探针结果
 * @returns 语气与文案；未通过时指出第一个失败的步骤
 */
export function probeSummary(result: ProbeResult): { tone: "success" | "danger"; text: string } {
  if (result.ok) return { tone: "success", text: "全部通过" };
  const failed = (result.steps ?? []).find((step) => !step.ok && !step.skipped);
  return {
    tone: "danger",
    text: failed ? `第 ${failed.index} 步「${failed.name}」未通过` : "未通过",
  };
}
