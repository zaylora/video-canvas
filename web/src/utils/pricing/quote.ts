import type { Capabilities, Pricing, PriceTier } from "@/api/model/type";

/**
 * 计价：与后端 backend/internal/provider/modelcfg/quote.go 是同一份算法，
 * 两边都跑 backend/internal/tests/testdata/pricing_vectors.json 里的测试向量，改算法时两边一起改。
 * 前端算出的数字只用于显示，下单时以后端算的为准。
 */

/** 一次提交选定的规格 */
export type PriceSpec = {
  /** 生成方式；文本、音频没有 */
  op?: string;
  /** 参考素材里是否有视频（且当前方式允许） */
  refVideo: boolean;
  /** 生成参数的取值（未开放的参数按默认值补齐） */
  params: Record<string, unknown>;
  /** 提示词字数（+ 固定系统提示字数），Token 计费按 1 字 = 1 Token 预估输入量 */
  promptChars: number;
};

/** 文本任务的实际 Token 用量 */
export type Usage = { input_tokens: number; output_tokens: number };

/** 条件值与规格取值比较：布尔按布尔比，其余按文本比（"5" 与 5 视为相同） */
function sameValue(got: unknown, want: unknown) {
  if (typeof want === "boolean") return got === want;
  if (got === undefined || got === null) return false;
  return String(got) === String(want);
}

function tierMatches(tier: PriceTier, spec: PriceSpec) {
  return Object.entries(tier.when).every(([key, want]) => {
    const got = key === "op" ? spec.op : key === "ref_video" ? spec.refVideo : spec.params[key];
    return sameValue(got, want);
  });
}

/** 命中的规格价格：on 且条件全部满足；条件最多的一条胜出，条件数相同取靠前的；没有任何条件的不参与 */
export function matchTier(pricing: Pricing, spec: PriceSpec): PriceTier | undefined {
  let best: PriceTier | undefined;
  for (const tier of pricing.tiers ?? []) {
    const size = Object.keys(tier.when ?? {}).length;
    if (!tier.on || size === 0 || !tierMatches(tier, spec)) continue;
    if (!best || size > Object.keys(best.when).length) best = tier;
  }
  return best;
}

/** 整数参数：数字或数字字符串都认，取不到为 0 */
function intParam(params: Record<string, unknown>, name: string) {
  const raw = params[name];
  const n = typeof raw === "number" ? raw : typeof raw === "string" ? Number(raw.trim()) : NaN;
  return Number.isFinite(n) ? Math.trunc(n) : 0;
}

/** 按百万 Token 单价算积分：只有这里会出现小数，向上取整，最少 1 积分 */
function tokenCredits(price: Pricing["token"], input: number, output: number) {
  if (!price) return 0;
  return Math.max(1, Math.ceil((input * price.in + output * price.out) / 1_000_000));
}

/** 每个任务要冻结的积分 */
export function quote(
  pricing: Pricing | undefined,
  caps: Capabilities | undefined,
  spec: PriceSpec,
) {
  if (!pricing) return 0;
  switch (pricing.billing) {
    case "per_call":
      return matchTier(pricing, spec)?.unit ?? pricing.unit ?? 0;
    case "per_second": {
      const rate = matchTier(pricing, spec)?.unit ?? pricing.per_second ?? 0;
      return rate * intParam(spec.params, "duration");
    }
    case "token":
      return tokenCredits(pricing.token, spec.promptChars, caps?.context?.output ?? 0);
    default:
      return 0;
  }
}

/** 任务成功时实际扣的积分：Token 计费按用量且不超过冻结额，没有用量按冻结额；其他计费方式扣冻结额 */
export function settle(pricing: Pricing, frozen: number, usage: Usage | null | undefined) {
  if (pricing.billing !== "token" || !usage) return frozen;
  return Math.min(tokenCredits(pricing.token, usage.input_tokens, usage.output_tokens), frozen);
}

/** 生成数量参数的名字；没有 fanout 参数为 undefined */
export const fanoutParam = (caps: Capabilities | undefined) =>
  Object.entries(caps?.params ?? {}).find(([, field]) => field.fanout)?.[0];

/** 生成数量：一次提交拆成几个任务；没有 fanout 参数为 1 */
export function fanoutCount(caps: Capabilities | undefined, params: Record<string, unknown>) {
  const name = fanoutParam(caps);
  if (!name) return 1;
  const n = intParam(params, name);
  return n > 0 ? n : 1;
}

/** 默认价格（不看规格），以及是否存在可能覆盖它的规格价格 */
function basePrice(pricing: Pricing) {
  const base = pricing.billing === "per_second" ? (pricing.per_second ?? 0) : (pricing.unit ?? 0);
  const units = (pricing.tiers ?? [])
    .filter((tier) => tier.on && Object.keys(tier.when ?? {}).length > 0)
    .map((tier) => tier.unit);
  return { base, min: Math.min(base, ...units), varies: units.some((unit) => unit !== base) };
}

/** 模型选择器上的价格文案：「10 积分」「2 积分/秒」「按 Token」，有更便宜的规格价时显示「起」 */
export function priceLabel(pricing: Pricing | undefined) {
  if (!pricing) return "";
  if (pricing.billing === "token") return "按 Token";
  const { min, varies } = basePrice(pricing);
  const unit = pricing.billing === "per_second" ? "积分/秒" : "积分";
  return `${min} ${unit}${varies ? "起" : ""}`;
}

/** 价格的排序值（用于「默认模型」等场景的比较）：最低单价 */
export const lowestUnit = (pricing: Pricing | undefined) =>
  !pricing || pricing.billing === "token" ? 0 : basePrice(pricing).min;
