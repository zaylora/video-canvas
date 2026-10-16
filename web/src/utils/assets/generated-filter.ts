import type { AssetKind, GeneratedQueryDto } from "@/api/asset-library/type";

import { timeRangeStart, type TimeRange } from "./time-range";

/** 来源筛选：全部 / 首页对话 / 某张画布 */
export type SourceFilter =
  | { type: "all" }
  | { type: "conversation" }
  | { type: "canvas"; canvasId: string };

/** 生成历史的筛选状态（类型标签另算） */
export type GeneratedFilter = {
  /** 来源 */
  source: SourceFilter;
  /** 时间范围 */
  range: TimeRange;
  /** 排序：最新优先 / 最早优先 */
  order: "desc" | "asc";
  /** 提示词关键字，已防抖 */
  q: string;
};

/** 没有任何筛选的初始状态 */
export const DEFAULT_GENERATED_FILTER: GeneratedFilter = {
  source: { type: "all" },
  range: "all",
  order: "desc",
  q: "",
};

/**
 * 筛选状态换成接口参数；时间范围在这里按当前时间换算，所以每次重新请求都是新鲜的起点。
 * @param kind 素材类型
 * @param filter 筛选状态
 * @param now 当前时间，测试时注入
 * @returns 不含游标与页大小的查询参数
 */
export function buildGeneratedQuery(
  kind: AssetKind,
  filter: GeneratedFilter,
  now: Date = new Date(),
): GeneratedQueryDto {
  return {
    kind,
    source: filter.source.type,
    canvasId: filter.source.type === "canvas" ? filter.source.canvasId : undefined,
    createdFrom: timeRangeStart(filter.range, now),
    order: filter.order,
    q: filter.q.trim() || undefined,
  };
}

/**
 * 是否有生效的筛选：「清除筛选」按钮只在这时出现。排序不算筛选，它不会让结果变少。
 * @param filter 筛选状态
 */
export function hasActiveFilter(filter: GeneratedFilter): boolean {
  return filter.source.type !== "all" || filter.range !== "all" || filter.q.trim() !== "";
}

/**
 * 来源转成下拉单选的值：all / conversation / canvas:<id>
 * @param source 来源筛选
 */
export function sourceToValue(source: SourceFilter): string {
  return source.type === "canvas" ? `canvas:${source.canvasId}` : source.type;
}

/**
 * 下拉单选的值转回来源筛选；不认识的值退回「全部」
 * @param value 下拉的值
 */
export function sourceFromValue(value: string): SourceFilter {
  if (value === "conversation") return { type: "conversation" };
  if (value.startsWith("canvas:") && value.length > "canvas:".length) {
    return { type: "canvas", canvasId: value.slice("canvas:".length) };
  }
  return { type: "all" };
}
