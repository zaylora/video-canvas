import type {
  MyPrompt,
  PromptChoice,
  PromptPage,
  PromptStep,
  PromptTemplate,
} from "@/api/drama/type";

/** 提示词的四个环节，按界面显示顺序 */
export const PROMPT_STEPS: { key: PromptStep; name: string }[] = [
  { key: "adapt", name: "剧本改编" },
  { key: "assets", name: "资产拆解" },
  { key: "asset_prompts", name: "资产图提示词" },
  { key: "video", name: "视频提示词" },
];

/** 我的提示词名称上限（字） */
export const PROMPT_NAME_MAX = 40;

/** 单页正文上限（字） */
export const PROMPT_BODY_MAX = 20_000;

/** 环节的中文名 */
export const promptStepName = (step: PromptStep): string =>
  PROMPT_STEPS.find((item) => item.key === step)?.name ?? step;

/** 剧本型输入的剧本改编行置灰的原因；其他情况返回 null */
export function adaptLockedReason(kind: "script" | "novel" | null): string | null {
  return kind === "script" ? "剧本直接导入，不改编" : null;
}

/** 两个提示词选择是不是同一个（来源 + id / 技能名） */
export function sameChoice(a: PromptChoice, b: PromptChoice): boolean {
  if (a.source !== b.source) return false;
  if (a.source === "mine") return a.id === b.id;
  if (a.source === "skill") return a.skill === b.skill;
  return true;
}

/**
 * 当前选择在下拉胶囊上显示的名字；来源（我的提示词 / 技能）已不存在时标「已删除 / 已停用」并报 missing
 * @param choice 当前选择
 * @param mine 该环节现有的我的提示词
 * @param skills 该环节现有的可用技能
 */
export function choiceLabel(
  choice: PromptChoice,
  mine: { id: string; name: string }[],
  skills: { name: string; title: string }[],
): { text: string; missing: boolean } {
  if (choice.source === "mine") {
    const hit = mine.find((item) => item.id === choice.id);
    return { text: `我的 · ${hit?.name ?? "已删除"}`, missing: !hit };
  }
  if (choice.source === "skill") {
    const hit = skills.find((item) => item.name === choice.skill);
    return { text: `技能 · ${hit?.title ?? "已停用"}`, missing: !hit };
  }
  return { text: "内置", missing: false };
}

/**
 * 提示词一行的显示状态：剧本型输入的剧本改编置灰并写原因
 * @param step 环节
 * @param kind 确认的原文类型
 * @param choice 当前选择
 * @param mine 该环节现有的我的提示词
 * @param skills 该环节现有的可用技能
 */
export function promptRowState(
  step: PromptStep,
  kind: "script" | "novel" | null,
  choice: PromptChoice,
  mine: { id: string; name: string }[],
  skills: { name: string; title: string }[],
): { label: string; disabledReason: string | null; missing: boolean } {
  if (step === "adapt" && kind === "script") {
    return { label: "剧本直接导入", disabledReason: adaptLockedReason(kind), missing: false };
  }
  const label = choiceLabel(choice, mine, skills);
  return { label: label.text, disabledReason: null, missing: label.missing };
}

/** 编辑器里正在编辑的一份提示词 */
export interface PromptDraft {
  /** 环节 */
  step: PromptStep;
  /** 已有条目的 id；新建为 null */
  id: string | null;
  /** 已有条目的乐观锁版本 */
  revision?: number;
  /** 名称 */
  name: string;
  /** 各页正文，键是页 key */
  bodies: Record<string, string>;
  /** 该环节的所有页（页签与恢复内置用） */
  pages: PromptPage[];
  /** 当前页 key */
  page: string;
}

const bodiesOf = (pages: PromptPage[]) =>
  Object.fromEntries(pages.map((page) => [page.key, page.body]));

/** 新建：名称为空，正文预填内置 */
export function draftFromTemplate(template: PromptTemplate): PromptDraft {
  return {
    step: template.step,
    id: null,
    name: "",
    bodies: bodiesOf(template.pages),
    pages: template.pages,
    page: template.pages[0]?.key ?? "",
  };
}

/** 编辑已有条目：用它的名称与正文，条目里缺的页用内置补齐 */
export function draftFromMine(template: PromptTemplate, mine: MyPrompt): PromptDraft {
  return {
    ...draftFromTemplate(template),
    id: mine.id,
    revision: mine.revision,
    name: mine.name,
    bodies: { ...bodiesOf(template.pages), ...mine.bodies },
  };
}

/** 把某一页恢复成内置正文，其他页保留用户改动 */
export function restoreBuiltin(
  draft: PromptDraft,
  template: PromptTemplate,
  page: string,
): PromptDraft {
  const builtin = template.pages.find((item) => item.key === page)?.body ?? "";
  return { ...draft, bodies: { ...draft.bodies, [page]: builtin } };
}

/**
 * 保存前的校验：名称必填、≤ 40 字、同环节不重名；每页正文不能空、不超 20,000 字。
 * 只做格式检查，不做语义校验（运行时靠输出 Schema 兜底）。
 * @param draft 草稿
 * @param existing 该环节现有的我的提示词（编辑自己时同名不算重复）
 * @returns 第一个问题的文案；没问题返回 null
 */
export function validatePromptDraft(
  draft: PromptDraft,
  existing: { id: string; name: string }[],
): string | null {
  const name = draft.name.trim();
  if (!name) return "请填写名称";
  if (name.length > PROMPT_NAME_MAX) return `名称不能超过 ${PROMPT_NAME_MAX} 字`;
  if (existing.some((item) => item.name === name && item.id !== draft.id)) {
    return "这个环节里已有同名提示词";
  }
  for (const page of draft.pages) {
    const body = draft.bodies[page.key] ?? "";
    const label = draft.pages.length > 1 ? `「${page.label}」正文` : "正文";
    if (!body.trim()) return `${label}不能为空`;
    if (body.length > PROMPT_BODY_MAX) {
      return `${label}不能超过 ${PROMPT_BODY_MAX.toLocaleString("en-US")} 字`;
    }
  }
  return null;
}
