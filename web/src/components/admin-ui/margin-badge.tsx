import type { ComponentProps } from "react";

import { Tag } from "@/components/admin-ui/tag";

/** 利润率 = (价格 − 成本) / 价格；价格为 0 时无从计算 */
export const marginOf = (price: number, cost: number) =>
  price > 0 ? (price - cost) / price : null;

/**
 * 利润率徽章：< 0 红（亏本）、< 20% 黄、其余绿；算不出时显示「-」。
 * @param price 用户价格（积分）
 * @param cost 积分成本
 */
function MarginBadge({
  price,
  cost,
  ...props
}: Omit<ComponentProps<typeof Tag>, "tone" | "children"> & { price: number; cost: number }) {
  const margin = marginOf(price, cost);
  const tone =
    margin === null ? "neutral" : margin < 0 ? "danger" : margin < 0.2 ? "warning" : "success";
  return (
    <Tag tone={tone} title="利润率 =（价格 − 成本）÷ 价格" {...props}>
      利润率 {margin === null ? "-" : `${Math.round(margin * 100)}%`}
    </Tag>
  );
}

export { MarginBadge };
