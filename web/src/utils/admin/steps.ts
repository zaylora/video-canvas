import type {
  StepBinding,
  StepCandidateInfo,
  StepMedia,
  StepSample,
  StepSummary,
  TrialCandidate,
  TrialView,
} from "@/api/admin/steps/type";

/** 技能正文超过这个字数会被截断（设计 §6.11） */
export const SKILL_TRUNCATE_CHARS = 16000;

/** 两个候选是不是同一个：都是内置，或技能名与版本都相同 */
export function sameCandidate(
  a: TrialCandidate | StepBinding | null | undefined,
  b: TrialCandidate | StepBinding | null | undefined,
): boolean {
  if (!a || !b || a.source !== b.source) return false;
  if (a.source === "builtin") return true;
  return a.skill_name === b.skill_name && a.version === b.version;
}

/** 候选的展示名：内置方法 / 技能名 vN */
export function candidateLabel(candidate: TrialCandidate | StepBinding): string {
  return candidate.source === "builtin"
    ? "内置方法"
    : `${candidate.skill_name ?? ""} v${candidate.version ?? 0}`;
}

/** 绑定的展示名；没有绑定（null）按内置 */
export function bindingLabel(binding: StepBinding | string | null | undefined): string {
  if (!binding) return "内置方法";
  if (typeof binding === "string") return binding;
  return candidateLabel(binding);
}

/** 候选列表里的角标：仅用正文（技能带脚本，脚本不执行）、会截断、已停用 */
export function candidateBadges(candidate: StepCandidateInfo): string[] {
  if (candidate.source === "builtin") return [];
  const badges: string[] = [];
  if (candidate.has_scripts) badges.push("仅用正文");
  if (candidate.chars > SKILL_TRUNCATE_CHARS) badges.push("会截断");
  if (!candidate.enabled) badges.push("已停用");
  return badges;
}

/** 绑定的技能被停用 / 版本被删，已自动退回内置的环节（页顶提示条用） */
export const fallbackSteps = (steps: readonly StepSummary[]): StepSummary[] =>
  steps.filter((step) => step.effective.fallback);

/** 试跑还没出结果 */
export const isTrialPending = (trial: TrialView | null | undefined): boolean =>
  !!trial && (trial.status === "queued" || trial.status === "running");

/**
 * 「设为生效」能不能点：必须是当前候选的试跑、已结束且格式校验通过，
 * 且候选不是当前绑定（绑定已退回内置时，选回原绑定可以重新生效）。
 * @param input.candidate 当前选中的候选
 * @param input.binding 环节当前绑定
 * @param input.fallback 绑定已退回内置
 * @param input.trial 当前候选的试跑；还没跑为 null
 */
export function activateState(input: {
  candidate: TrialCandidate;
  binding: StepBinding;
  fallback: boolean;
  trial: TrialView | null;
}): { disabled: boolean; reason: string } {
  const { candidate, binding, fallback, trial } = input;
  if (!trial || !sameCandidate(trial.candidate, candidate)) {
    return { disabled: true, reason: "先试跑这个候选" };
  }
  if (isTrialPending(trial)) return { disabled: true, reason: "试跑进行中" };
  if (trial.status === "failed") return { disabled: true, reason: "试跑调用失败，没有结果" };
  if (!trial.passed) return { disabled: true, reason: "候选没有通过格式校验" };
  if (sameCandidate(candidate, binding) && !fallback) {
    return { disabled: true, reason: "候选就是当前绑定" };
  }
  return { disabled: false, reason: "" };
}

/** 试跑能选哪些文本模型：环节要看图 / 看视频时必须是能看图的模型 */
export function trialModelAllowed(media: StepMedia, model: { vision?: boolean }): boolean {
  return media === "none" || model.vision === true;
}

/** 需要带参考片样例才能试跑的环节：拆解参考片、跟拍改写 */
export const stepNeedsRefSample = (stepKey: string): boolean =>
  stepKey.endsWith(".reverse") || stepKey.endsWith(".follow");

/** 样例能不能选：需要参考片的环节只能选带参考片的样例 */
export const sampleAllowed = (stepKey: string, sample: Pick<StepSample, "needs_ref">): boolean =>
  !stepNeedsRefSample(stepKey) || sample.needs_ref;

/**
 * 绑定历史里的「换之前 / 换之后」转成候选，用来「恢复」：
 * 对象直接用；展示串认「内置方法」和「技能名 vN」；认不出返回 null。
 */
export function candidateOfHistory(
  value: StepBinding | string | null | undefined,
): TrialCandidate | null {
  if (!value) return { source: "builtin" };
  if (typeof value !== "string") return value;
  if (value === "内置方法") return { source: "builtin" };
  const match = /^(.+) v(\d+)$/.exec(value);
  return match ? { source: "skill", skill_name: match[1], version: Number(match[2]) } : null;
}
