import type { ActivityDay } from "@/api/me/type";

/**
 * 热力图的纯函数（设计 docs/design/个人中心 §6.4）：补齐日期、按周日对齐成 7 行 × N 列、
 * 四分位分档、连续天数、月份标签、tooltip 文案和方向键移动。
 * 日期一律用 YYYY-MM-DD 字符串，内部按 UTC 换算，和浏览器时区无关——
 * 分天已经由后端按用户时区做完了，这里只做日历运算。
 */

/** 一天的毫秒数 */
const DAY_MS = 86_400_000;

/** 星期几的中文名，下标 0 是周日 */
const WEEKDAYS = ["日", "一", "二", "三", "四", "五", "六"];

/** tooltip 第二行里各类型的名字和顺序 */
const KIND_LABELS: [keyof Pick<ActivityDay, "image" | "video" | "audio" | "text">, string][] = [
  ["image", "图片"],
  ["video", "视频"],
  ["audio", "音频"],
  ["text", "文本"],
];

/** 档位：0 表示无生成，1–4 越来越深 */
export type HeatLevel = 0 | 1 | 2 | 3 | 4;

/** 网格里的一格 */
export interface HeatCell {
  /** 日期 YYYY-MM-DD */
  date: string;
  /** 行：0 周日 … 6 周六 */
  row: number;
  /** 列：从 0 开始，越往右越近 */
  col: number;
  /** 当天任务数 */
  count: number;
  /** 颜色档位 */
  level: HeatLevel;
  /** 当天按类型拆分的原始数据；0 次时为 undefined */
  day?: ActivityDay;
}

/** 月份标签 */
export interface MonthLabel {
  /** 落在第几列 */
  col: number;
  /** 文案，例如「10月」 */
  label: string;
}

/** 格子坐标 */
export interface CellPos {
  /** 行：0 周日 … 6 周六 */
  row: number;
  /** 列 */
  col: number;
}

/** 补齐后的热力图模型 */
export interface HeatmapModel {
  /** 按列存放，每列 7 格；null 是区间外的占位（不渲染） */
  columns: (HeatCell | null)[][];
  /** 月份标签 */
  months: MonthLabel[];
  /** 区间内任务总数 */
  total: number;
  /** 有生成的天数 */
  activeDays: number;
  /** 最长连续天数 */
  longestStreak: number;
  /** 当前连续天数：今天为 0 时从昨天算起 */
  currentStreak: number;
  /** 默认可聚焦的格子：最后一天；区间为空时为 null */
  lastCell: CellPos | null;
}

/**
 * 日期字符串转成 UTC 毫秒
 * @param date YYYY-MM-DD
 */
const toMs = (date: string) => Date.parse(`${date}T00:00:00Z`);

/**
 * UTC 毫秒转成日期字符串
 * @param ms UTC 毫秒
 */
const toDate = (ms: number) => new Date(ms).toISOString().slice(0, 10);

/**
 * 日期加减天数
 * @param date YYYY-MM-DD
 * @param days 天数，可为负
 * @returns 新日期 YYYY-MM-DD
 */
export function addDays(date: string, days: number): string {
  return toDate(toMs(date) + days * DAY_MS);
}

/**
 * 星期几：0 周日 … 6 周六
 * @param date YYYY-MM-DD
 */
const weekday = (date: string) => new Date(toMs(date)).getUTCDay();

/**
 * 「最近一年」的起点：今天所在周的周日往前 52 周，合起来正好 53 列（与 GitHub 一致）
 * @param end 今天 YYYY-MM-DD
 * @returns 起点 YYYY-MM-DD，总是周日
 */
export function recentYearStart(end: string): string {
  return addDays(end, -weekday(end) - 52 * 7);
}

/**
 * 按非零天的分布生成分档函数。0 次是 0 档；非零天排序后取 Q1、Q2、Q3，
 * ≤Q1 为 1 档，≤Q2 为 2 档，≤Q3 为 3 档，>Q3 为 4 档。
 * 非零天少于 4 天时四分位没有意义：1 次为 1 档，其余按最大值线性分到 2–4 档。
 * @param counts 区间内所有非零天的次数（顺序不限）
 * @returns 次数 → 档位
 */
export function heatLevels(counts: number[]): (count: number) => HeatLevel {
  const sorted = counts.filter((value) => value > 0).sort((a, b) => a - b);
  const n = sorted.length;
  if (n === 0) return () => 0;
  if (n < 4) {
    const max = sorted[n - 1];
    return (count) => {
      if (count <= 0) return 0;
      if (count === 1 || max === 1) return 1;
      const ratio = (count - 1) / (max - 1);
      return Math.max(2, Math.min(4, 2 + Math.round(ratio * 2))) as HeatLevel;
    };
  }
  const quantile = (p: number) => sorted[Math.floor((n - 1) * p)];
  const [q1, q2, q3] = [quantile(0.25), quantile(0.5), quantile(0.75)];
  return (count) => {
    if (count <= 0) return 0;
    if (count <= q1) return 1;
    if (count <= q2) return 2;
    if (count <= q3) return 3;
    return 4;
  };
}

/**
 * 连续天数。最长连续在整个区间里找；当前连续从 end 往前数，end 当天为 0 时从前一天开始（与 GitHub 一致）
 * @param input 区间与每日次数
 * @returns 最长连续与当前连续天数
 */
export function streaks(input: { start: string; end: string; counts: Map<string, number> }): {
  longest: number;
  current: number;
} {
  const { start, end, counts } = input;
  const has = (date: string) => (counts.get(date) ?? 0) > 0;
  let longest = 0;
  let run = 0;
  for (let date = start; date <= end; date = addDays(date, 1)) {
    run = has(date) ? run + 1 : 0;
    longest = Math.max(longest, run);
  }
  let current = 0;
  let cursor = has(end) ? end : addDays(end, -1);
  while (cursor >= start && has(cursor)) {
    current += 1;
    cursor = addDays(cursor, -1);
  }
  return { longest, current };
}

/**
 * 「2026年10月8日」
 * @param date YYYY-MM-DD
 */
const chineseDate = (date: string) => {
  const [y, m, d] = date.split("-").map(Number);
  return `${y}年${m}月${d}日`;
};

/**
 * tooltip 两行文案：有生成时为「日期 星期X · N 次生成」+ 按类型拆分；0 次时为「日期 · 无生成」，第二行为空
 * @param date YYYY-MM-DD
 * @param day 当天数据；0 次时不传
 * @returns [第一行, 第二行]
 */
export function tooltipLines(date: string, day: ActivityDay | undefined): [string, string] {
  if (!day || day.count <= 0) return [`${chineseDate(date)} · 无生成`, ""];
  const parts = KIND_LABELS.filter(([key]) => day[key] > 0)
    .map(([key, label]) => `${label} ${day[key]}`)
    .join(" · ");
  return [`${chineseDate(date)} 星期${WEEKDAYS[weekday(date)]} · ${day.count} 次生成`, parts];
}

/**
 * 格子的 aria-label：与 tooltip 文案一致，两行用逗号连起来
 * @param date YYYY-MM-DD
 * @param day 当天数据；0 次时不传
 */
export function cellLabel(date: string, day: ActivityDay | undefined): string {
  const [first, second] = tooltipLines(date, day);
  return second ? `${first}，${second}` : first;
}

/**
 * 把后端返回的稀疏数据补齐成网格。网格从 start 所在周的周日开始，到 end 所在周结束；
 * start 之前、end 之后的格子是占位（null），不渲染——未来日期不画，自然年 1 月 1 日之前也不画。
 * @param input 区间（闭区间）与只含非零天的数据
 * @returns 补齐后的模型
 */
export function buildHeatmap(input: {
  start: string;
  end: string;
  days: ActivityDay[];
}): HeatmapModel {
  const { start, end } = input;
  const byDate = new Map<string, ActivityDay>();
  for (const day of input.days) {
    if (day.date >= start && day.date <= end && day.count > 0) byDate.set(day.date, day);
  }
  const level = heatLevels([...byDate.values()].map((day) => day.count));
  const gridStart = addDays(start, -weekday(start));
  const totalDays = Math.round((toMs(end) - toMs(gridStart)) / DAY_MS) + 1;
  const columnCount = Math.max(0, Math.ceil(totalDays / 7));

  const columns: (HeatCell | null)[][] = [];
  const months: MonthLabel[] = [];
  let lastMonth = -1;
  let total = 0;
  let lastCell: CellPos | null = null;

  for (let col = 0; col < columnCount; col += 1) {
    const column: (HeatCell | null)[] = [];
    let firstInRange: string | null = null;
    for (let row = 0; row < 7; row += 1) {
      const date = addDays(gridStart, col * 7 + row);
      if (date < start || date > end) {
        column.push(null);
        continue;
      }
      firstInRange ??= date;
      const day = byDate.get(date);
      const count = day?.count ?? 0;
      total += count;
      column.push({ date, row, col, count, level: level(count), day });
      lastCell = { row, col };
    }
    columns.push(column);
    if (firstInRange) {
      const month = Number(firstInRange.slice(5, 7));
      const dayOfMonth = Number(firstInRange.slice(8, 10));
      /** 首列总是标；之后只在每月第一周所在的列标，免得标签挤在一起 */
      if (month !== lastMonth && (col === 0 || dayOfMonth <= 7)) {
        months.push({ col, label: `${month}月` });
        lastMonth = month;
      }
    }
  }

  const counts = new Map([...byDate].map(([date, day]) => [date, day.count]));
  const { longest, current } = streaks({ start, end, counts });
  return {
    columns,
    months,
    total,
    activeDays: byDate.size,
    longestStreak: longest,
    currentStreak: current,
    lastCell,
  };
}

/**
 * 键盘移动焦点（ARIA grid）：方向键逐格，Home/End 到本行首尾，PageUp/PageDown 到本列首尾。
 * 目标是占位或越界时不动。
 * @param model 热力图模型
 * @param from 当前格子
 * @param key KeyboardEvent.key
 * @returns 目标格子；不需要处理或不能移动时返回 null
 */
export function moveCell(model: HeatmapModel, from: CellPos, key: string): CellPos | null {
  const { columns } = model;
  const exists = (row: number, col: number) => !!columns[col]?.[row];
  const at = (row: number, col: number) => (exists(row, col) ? { row, col } : null);
  const { row, col } = from;
  switch (key) {
    case "ArrowLeft":
      return at(row, col - 1);
    case "ArrowRight":
      return at(row, col + 1);
    case "ArrowUp":
      return at(row - 1, col);
    case "ArrowDown":
      return at(row + 1, col);
    case "Home":
      for (let c = 0; c < columns.length; c += 1) if (exists(row, c)) return { row, col: c };
      return null;
    case "End":
      for (let c = columns.length - 1; c >= 0; c -= 1) if (exists(row, c)) return { row, col: c };
      return null;
    case "PageUp":
      for (let r = 0; r < 7; r += 1) if (exists(r, col)) return { row: r, col };
      return null;
    case "PageDown":
      for (let r = 6; r >= 0; r -= 1) if (exists(r, col)) return { row: r, col };
      return null;
    default:
      return null;
  }
}
