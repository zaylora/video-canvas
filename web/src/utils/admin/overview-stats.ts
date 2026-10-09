import type { AdminStatsDay, ChannelLoad, ChannelView, PluginView } from "@/api/admin/ai/type";

import { channelHealth, type HealthTone } from "./health";

/**
 * 总览页图表的纯逻辑：统计口径的汇总、刻度、占比归并、渠道负载条的比例。
 * 页面上所有数字都来自同一份统计，三个桶（成功 / 失败 / 其他）之和就是当天的任务数。
 */

/**
 * 一天的任务总数
 * @param day 当天的三个桶
 * @returns 三个桶之和
 */
export const dayTotal = (day: AdminStatsDay) => day.succeeded + day.failed + day.other;

/**
 * 今天与昨天的任务数。daily 最后一项是今天、倒数第二项是昨天，不够时按 0 处理。
 * @param daily 按日期升序的每日统计
 * @returns 今天与昨天的任务总数
 */
export function todayAndYesterday(daily: readonly AdminStatsDay[]) {
  const today = daily.at(-1);
  const yesterday = daily.at(-2);
  return { today: today ? dayTotal(today) : 0, yesterday: yesterday ? dayTotal(yesterday) : 0 };
}

/**
 * 成功率（百分数）：成功 ÷ (成功 + 失败)。“其他”（取消与进行中）结果未定，不进分母。
 * @param daily 每日统计
 * @returns 0–100 的百分数；一个已结束的任务都没有时返回 null（页面显示“—”，而不是 0%）
 */
export function successRate(daily: readonly AdminStatsDay[]): number | null {
  let ok = 0;
  let failed = 0;
  for (const day of daily) {
    ok += day.succeeded;
    failed += day.failed;
  }
  return ok + failed === 0 ? null : (ok / (ok + failed)) * 100;
}

/** 占比图的一项 */
export type ShareItem = {
  /** 稳定 key（模型 key 或类型名） */
  key: string;
  /** 展示名 */
  label: string;
  /** 次数 */
  count: number;
  /** 是被合并出来的“其他”：用中性色，不占类别色 */
  other?: boolean;
};

/**
 * 取前 n 名，其余合并成一项“其他”。
 * 只多出一项时不合并：一个“其他”里只装着一项没有意义，直接列出来。
 * @param items 已按次数降序排好的全量
 * @param n 保留的名次
 * @returns 新数组，不修改入参
 */
export function topN(items: readonly ShareItem[], n: number): ShareItem[] {
  if (items.length <= n + 1) return [...items];
  const rest = items.slice(n).reduce((sum, item) => sum + item.count, 0);
  return [...items.slice(0, n), { key: "__other", label: "其他", count: rest, other: true }];
}

/**
 * Y 轴刻度：约 4 段，每段取 1 / 2 / 5 × 10ⁿ（数值大时再加 2.5），上限不小于最大值，刻度都是整数。
 * @param maxValue 数据里的最大值
 * @returns 轴上限与从 0 开始的刻度；没有数据时给 0–4，图表仍然有网格
 */
export function niceTicks(maxValue: number): { max: number; ticks: number[] } {
  const segments = 4;
  if (maxValue <= 0) return { max: segments, ticks: [0, 1, 2, 3, 4] };
  const raw = Math.max(maxValue, segments) / segments;
  const base = 10 ** Math.floor(Math.log10(raw));
  const f = raw / base;
  const steps = base >= 10 ? [1, 2, 2.5, 5, 10] : [1, 2, 5, 10];
  const step = (steps.find((s) => f <= s) ?? 10) * base;
  const count = Math.ceil(maxValue / step);
  return { max: step * count, ticks: Array.from({ length: count + 1 }, (_, i) => i * step) };
}

/**
 * X 轴要显示标签的日期：7 天全部显示；更长时从今天往前每 5 天一个，保证今天一定有标签。
 * @param dates 按升序排好的日期
 * @param every 间隔
 * @returns 要显示标签的日期
 */
export function thinTicks(dates: readonly string[], every = 5): string[] {
  if (dates.length <= 7) return [...dates];
  return dates.filter((_, i) => (dates.length - 1 - i) % every === 0);
}

/** 解析 YYYY-MM-DD，不经过带时区的 Date 解析，避免被运行环境的时区挪一天 */
function parseDate(date: string) {
  const [y, m, d] = date.split("-").map(Number);
  return { y, m, d };
}

/**
 * X 轴标签：月/日，不补零
 * @param date YYYY-MM-DD
 * @returns 如 10/4
 */
export function formatMonthDay(date: string) {
  const { m, d } = parseDate(date);
  return `${m}/${d}`;
}

/**
 * Tooltip 标题：月日加星期
 * @param date YYYY-MM-DD
 * @returns 如“10 月 4 日 · 周日”
 */
export function formatTooltipDate(date: string) {
  const { y, m, d } = parseDate(date);
  return `${m} 月 ${d} 日 · 周${"日一二三四五六"[new Date(y, m - 1, d).getDay()]}`;
}

/** 渠道负载条的一行 */
export type LoadRow = {
  /** 渠道 key */
  key: string;
  /** 渠道显示名 */
  name: string;
  /** 同时生成数 */
  running: number;
  /** 排队数 */
  waiting: number;
  /** 同时生成上限；0 表示不限 */
  max: number;
  /** 渠道此刻能不能接任务（启用、插件启用、Key 已设置） */
  usable: boolean;
  /** 不可用时的短标签（已停用 / 不可用 / 未设 Key）；可用时为“可用” */
  label: string;
  /** 健康色调 */
  tone: HealthTone;
  /** 满载：有上限且生成中已达上限 */
  full: boolean;
  /** 空闲：没有生成中也没有排队 */
  idle: boolean;
  /** 生成中占整条的比例（0–1） */
  runningRatio: number;
  /** 生成中 + 排队占整条的比例（0–1），叠在生成中的后面 */
  queuedRatio: number;
};

/**
 * 渠道负载条的数据。
 * - 分母：有上限时取 max(上限, 生成中 + 排队)，条不会溢出；不限时取所有可用渠道里最大的占用，让几条之间可比；
 * - 不可用的渠道不画负载（它接不了任务），带原因标签；
 * - 排序：可用的按“生成中 + 排队”降序，不可用的排最后，同样大按 key 升序。
 * @param channels 渠道清单
 * @param plugins 插件清单（判断渠道是否可用要用）
 * @param loads 各渠道负载，没有未完成任务的渠道不在其中
 * @returns 排好序的行
 */
export function loadRows(
  channels: readonly ChannelView[],
  plugins: readonly PluginView[],
  loads: readonly ChannelLoad[],
): LoadRow[] {
  const loadOf = new Map(loads.map((l) => [l.channel, l]));
  const base = channels.map((channel) => {
    const health = channelHealth(channel, plugins);
    const load = loadOf.get(channel.key);
    return {
      channel,
      health,
      usable: health.tone === "ok",
      running: load?.running ?? 0,
      waiting: load?.waiting ?? 0,
    };
  });
  const busiest = Math.max(1, ...base.filter((b) => b.usable).map((b) => b.running + b.waiting));

  const rows = base.map(({ channel, health, usable, running, waiting }): LoadRow => {
    const max = channel.rate_limit?.max_running ?? 0;
    const denom = max > 0 ? Math.max(max, running + waiting) : busiest;
    return {
      key: channel.key,
      name: channel.name,
      running,
      waiting,
      max,
      usable,
      label: health.label,
      tone: health.tone,
      full: usable && max > 0 && running >= max,
      idle: running === 0 && waiting === 0,
      runningRatio: usable ? running / denom : 0,
      queuedRatio: usable ? (running + waiting) / denom : 0,
    };
  });
  return rows.sort((a, b) => {
    if (a.usable !== b.usable) return a.usable ? -1 : 1;
    const busy = b.running + b.waiting - (a.running + a.waiting);
    return a.usable && busy !== 0 ? busy : a.key.localeCompare(b.key);
  });
}
