/** 生成历史的时间筛选 */
export type TimeRange = "all" | "today" | "week" | "month";

/** 各范围往前数几个本地日（含今天） */
const DAYS: Record<Exclude<TimeRange, "all">, number> = { today: 1, week: 7, month: 30 };

/**
 * 把时间筛选换成接口的 created_from：按本地时区算起点，所以「今天」是用户这边的 0 点，
 * 而不是 UTC 0 点；「近 7 天」含今天、往前共 7 个本地日。
 * @param range 时间筛选
 * @param now 当前时间，测试时注入
 * @returns RFC3339（UTC）起点；「全部」返回 undefined，不传这个参数
 */
export function timeRangeStart(range: TimeRange, now: Date = new Date()): string | undefined {
  if (range === "all") return undefined;
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (DAYS[range] - 1));
  return start.toISOString();
}
