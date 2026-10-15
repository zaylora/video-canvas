import type { Billing, PriceCost, PriceTier } from "@/api/model/type.d";

/** 积分成本里与价格同单位的那个数：没开成本为 undefined，开了没填按 0（Token 计费不走这里） */
export const costValue = (billing: Billing, cost: PriceCost | undefined) =>
  !cost?.on ? undefined : billing === "per_second" ? (cost.per_second ?? 0) : (cost.unit ?? 0);

/** 写入同单位的成本并开启：按秒写 per_second，其余写 unit */
export const withCostValue = (
  billing: Billing,
  cost: PriceCost | undefined,
  value: number,
): PriceCost => ({ ...cost, on: true, [billing === "per_second" ? "per_second" : "unit"]: value });

/**
 * 按规格定价时不展示默认价格，没命中任何规格时按这个价收取：启用规格里最低的价（都没启用取全部里最低）。
 * 没有规格返回 undefined。
 */
export function fallbackUnit(tiers: PriceTier[]) {
  const live = tiers.filter((tier) => tier.on);
  const pool = live.length ? live : tiers;
  return pool.length ? Math.min(...pool.map((tier) => tier.unit)) : undefined;
}
