import type { AnnouncementDto } from "@/api/announcement/type";

/**
 * 首页公告通栏的规则（设计稿 docs/design/公告/公告.md §6.1）。
 * 这里只放纯函数和常量，组件和 hook 只负责接线，规则都能单测。
 */

/** 标题放得下时每条停留 5 秒 */
export const ROTATE_MS = 5000;
/** 标题放不下时：开头先停 1.5 秒让人读开头，滚到末尾再停 1.5 秒 */
export const MARQUEE_HOLD_MS = 1500;
/** 滚动速度：每秒 60px，够慢、能边滚边读 */
export const MARQUEE_SPEED = 60;
/** 滚到末尾时多走 16px，让最后一个字离开右侧渐隐遮罩 */
export const MARQUEE_TAIL_PX = 16;
/** 溢出不超过 2px 视为放得下：字体渲染的亚像素误差不该触发滚动 */
const OVERFLOW_TOLERANCE_PX = 2;
/** 回到窗口时，距上次拉取满 5 分钟才重新拉 */
export const REFETCH_AFTER_MS = 5 * 60_000;
/** 当前会话最多记 50 条关闭记录，挤掉最早关闭的 */
export const DISMISS_LIMIT = 50;
/** 关闭记录（sessionStorage）和上次公告缓存（localStorage）的键 */
export const DISMISSED_KEY = "announcement-dismissed";
export const CACHE_KEY = "announcement-cache";

/**
 * 关闭记录：[公告 id, 关闭时的版本]，按关闭先后排列。
 * 不用 { id: version } 对象：JS 对象里整数样子的键永远按数值升序排，记不住先后，就没法挤掉「最早关闭的」
 */
export type DismissedList = Array<[id: number, version: number]>;

/**
 * 这条公告当前版本是否被关闭过
 * @param list 关闭记录
 * @param id 公告 ID
 * @param version 公告当前版本
 */
export function isDismissed(list: DismissedList, id: number, version: number): boolean {
  return list.some(([k, v]) => k === id && v === version);
}

/**
 * 当前要显示的公告：去掉已过结束时间的（恰好此刻算已结束），去掉当前会话里关闭过且版本没变的。
 * 顺序沿用服务端的顺序（后台排序）。
 * @param items 服务端返回（或本机缓存）的公告
 * @param dismissed 当前会话的关闭记录
 * @param now 当前毫秒时间戳
 */
export function visibleAnnouncements(
  items: AnnouncementDto[],
  dismissed: DismissedList,
  now: number,
): AnnouncementDto[] {
  return items.filter(
    (item) =>
      (item.endsAt === null || item.endsAt > now) && !isDismissed(dismissed, item.id, item.version),
  );
}

/**
 * 解析当前会话的关闭记录；坏数据回落空记录，只保留 [数字, 数字] 的条目
 * @param raw sessionStorage 里的原文
 */
export function parseDismissed(raw: string | null): DismissedList {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    return value.filter(
      (entry): entry is [number, number] =>
        Array.isArray(entry) &&
        entry.length === 2 &&
        Number.isFinite(entry[0]) &&
        Number.isFinite(entry[1]),
    );
  } catch {
    return [];
  }
}

/**
 * 记下一条关闭：返回新数组（不改原数组）；同一条再关会挪到最后；超过上限挤掉最早关闭的
 * @param list 现有关闭记录
 * @param id 公告 ID
 * @param version 关闭时的版本
 */
export function dismiss(list: DismissedList, id: number, version: number): DismissedList {
  const next: DismissedList = list.filter(([k]) => k !== id);
  next.push([id, version]);
  return next.slice(-DISMISS_LIMIT);
}

/** 标题放不下时的滚动计划：位移、滚动耗时、含首尾停顿的总时长（毫秒） */
export interface MarqueePlan {
  travel: number;
  runMs: number;
  totalMs: number;
}

/**
 * 根据标题溢出的宽度算滚动计划；放得下返回 null
 * @param overflowPx 标题宽度减去可视宽度
 */
export function marqueePlan(overflowPx: number): MarqueePlan | null {
  if (overflowPx <= OVERFLOW_TOLERANCE_PX) return null;
  const travel = Math.ceil(overflowPx + MARQUEE_TAIL_PX);
  const runMs = Math.round((travel / MARQUEE_SPEED) * 1000);
  return { travel, runMs, totalMs: MARQUEE_HOLD_MS + runMs + MARQUEE_HOLD_MS };
}

/**
 * 一条公告停留多久再切下一条：放得下 5 秒；放不下取 5 秒和滚完所需时间的较大者
 * @param plan 滚动计划，放得下为 null
 */
export function dwellMs(plan: MarqueePlan | null): number {
  return plan ? Math.max(ROTATE_MS, plan.totalMs) : ROTATE_MS;
}

/**
 * 回到窗口时要不要重新拉取：从没拉过要拉，距上次满 5 分钟要拉
 * @param lastAt 上次成功拉取的毫秒时间戳
 * @param now 当前毫秒时间戳
 */
export function shouldRefetch(lastAt: number | null, now: number): boolean {
  return lastAt === null || now - lastAt >= REFETCH_AFTER_MS;
}

/** 判断一个值是不是形状完整的公告（本机缓存可能被改坏或来自旧版本） */
function isAnnouncement(value: unknown): value is AnnouncementDto {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.id === "number" &&
    typeof v.title === "string" &&
    (v.endsAt === null || typeof v.endsAt === "number") &&
    typeof v.version === "number"
  );
}

/**
 * 解析本机缓存的上一次公告：首屏先用它渲染，避免每次进首页整页往下跳 40px；坏数据回落空数组
 * @param raw localStorage 里的原文
 */
export function parseCachedAnnouncements(raw: string | null): AnnouncementDto[] {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    return Array.isArray(value) ? value.filter(isAnnouncement) : [];
  } catch {
    return [];
  }
}
