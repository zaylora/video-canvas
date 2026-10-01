import type { Capabilities, ParamField, ParamOption, Pricing } from "@/api/model/type";

/**
 * 插件在导入草稿里给的生成参数预填建议（契约见 backend/docs/plugin-contract.md「导入草稿的参数预填建议」）。
 * 只在导入那一刻用：按种类套默认能力模板后，覆盖模板里同名参数的取值设置。
 */
export type ParamHint = {
  /** enum：可选值 */
  options?: ParamOption[];
  /** 默认值 */
  default?: ParamOption | boolean;
  /** number：最小 / 最大 / 步长 */
  min?: number;
  max?: number;
  step?: number;
  /** 是否开放给用户 */
  open?: boolean;
  /** 模型没有这一项：从模板里去掉 */
  remove?: boolean;
};

export type ParamHints = Record<string, ParamHint>;

const isOption = (value: unknown): value is ParamOption =>
  (typeof value === "string" && value.trim() !== "") ||
  (typeof value === "number" && Number.isFinite(value));

const isInt = (value: unknown): value is number =>
  typeof value === "number" && Number.isInteger(value);

/** 一条建议覆盖到参数上；只覆盖这种参数类型认识的字段，其余忽略 */
function applyOne(field: ParamField, hint: ParamHint): ParamField {
  const next: ParamField = { ...field };
  if (typeof hint.open === "boolean") next.open = hint.open;
  switch (field.type) {
    case "enum": {
      const options = hint.options?.filter(isOption);
      if (options?.length) next.options = options;
      if (isOption(hint.default)) next.default = hint.default;
      // 默认值必须在可选值里：建议没给或给错时取第一个可选值
      const list = next.options ?? [];
      if (!list.some((item) => String(item) === String(next.default))) next.default = list[0];
      break;
    }
    case "number": {
      if (isInt(hint.min)) next.min = hint.min;
      if (isInt(hint.max)) next.max = hint.max;
      if (isInt(hint.step) && hint.step > 0) next.step = hint.step;
      if (isInt(hint.default)) next.default = hint.default;
      // 默认值落到范围里（超出平台固定范围的不修正，编辑器照常标红）
      const value = typeof next.default === "number" ? next.default : next.min;
      if (value !== undefined && next.min !== undefined && next.max !== undefined)
        next.default = Math.min(Math.max(value, next.min), next.max);
      break;
    }
    case "boolean":
      if (typeof hint.default === "boolean") next.default = hint.default;
      break;
  }
  return next;
}

/** 规格价格的条件还成不成立：引用的参数已被去掉，或取值已不在可选值里，就不成立 */
function tierStillValid(when: Record<string, unknown>, params: Record<string, ParamField>) {
  return Object.entries(when).every(([key, value]) => {
    if (key === "op" || key === "ref_video") return true;
    const field = params[key];
    if (!field) return false;
    if (field.type !== "enum") return true;
    return (field.options ?? []).some((item) => String(item) === String(value));
  });
}

/**
 * 把预填建议覆盖到能力模板上，并清掉因此失效的规格价格（比如去掉了清晰度，按清晰度分档的价格就没有意义）。
 * 不认识的参数名（模板里没有）不新增，原样列在 ignored 里，导入时提示运营。
 */
export function applyParamHints(
  caps: Capabilities,
  pricing: Pricing,
  hints: ParamHints | null | undefined,
): { capabilities: Capabilities; pricing: Pricing; ignored: string[] } {
  const ignored: string[] = [];
  if (!hints || Object.keys(hints).length === 0) return { capabilities: caps, pricing, ignored };
  const params: Record<string, ParamField> = {};
  const source = caps.params ?? {};
  for (const name of Object.keys(hints)) if (!(name in source)) ignored.push(name);
  // 按模板的书写顺序重建，保持参数面板的显示顺序
  for (const [name, field] of Object.entries(source)) {
    const hint = hints[name];
    if (!hint) params[name] = field;
    else if (!hint.remove) params[name] = applyOne(field, hint);
  }
  const tiers = pricing.tiers?.filter((tier) => tierStillValid(tier.when ?? {}, params));
  return {
    capabilities: { ...caps, params },
    pricing: { ...pricing, tiers: tiers?.length ? tiers : undefined },
    ignored,
  };
}

/** 草稿里模板不认识的建议参数名（导入弹窗提示用） */
export const ignoredHints = (caps: Capabilities, hints: ParamHints | null | undefined) =>
  Object.keys(hints ?? {}).filter((name) => !(name in (caps.params ?? {})));
