import { EASTER_LINES, type EasterKind } from "@/constants/pet";

/** 只用到 getItem / setItem，方便测试时传内存实现 */
export type StampStorage = Pick<Storage, "getItem" | "setItem">;

const FIRST_OPEN_KEY = "video-canvas-pet-last-open";
const NIGHT_KEY = "video-canvas-pet-last-night";

const pad = (n: number) => String(n).padStart(2, "0");

/** 本地日期，形如 2026-11-06 */
export const dayKey = (d: Date): string =>
  `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

/** 23:00–05:00 算夜间 */
export const isNightHour = (d: Date): boolean => d.getHours() >= 23 || d.getHours() < 5;

/** 夜晚编号：零点之后仍算前一天晚上；白天是 null */
export function nightKey(d: Date): string | null {
  if (!isNightHour(d)) return null;
  if (d.getHours() < 5) return dayKey(new Date(d.getFullYear(), d.getMonth(), d.getDate() - 1));
  return dayKey(d);
}

/** 浏览器的 localStorage；被禁用、隐私模式下取不到时返回 null */
export function browserStorage(): StampStorage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

/**
 * 记录一次「今天已经触发过」：和已存的值不同才算第一次。
 * 读不到（存储不可用）按「不触发」处理，免得每次打开都冒；写失败不影响本次结果。
 */
function claim(storage: StampStorage | null, key: string, value: string): boolean {
  if (!storage) return false;
  let stored: string | null;
  try {
    stored = storage.getItem(key);
  } catch {
    return false;
  }
  if (stored === value) return false;
  try {
    storage.setItem(key, value);
  } catch {
    // 写不进去也照常触发这一次
  }
  return true;
}

/** 当天第一次打开画布 */
export const claimFirstOpenToday = (
  now: Date = new Date(),
  storage: StampStorage | null = browserStorage(),
): boolean => claim(storage, FIRST_OPEN_KEY, dayKey(now));

/** 23:00–05:00 每晚第一次；白天不触发，也不写记录 */
export function claimNightEgg(
  now: Date = new Date(),
  storage: StampStorage | null = browserStorage(),
): boolean {
  const night = nightKey(now);
  return night !== null && claim(storage, NIGHT_KEY, night);
}

/** 从某一类彩蛋里随机选一句 */
export function pickEasterLine(kind: EasterKind, rand: () => number = Math.random): string {
  const lines = EASTER_LINES[kind];
  return lines[Math.min(lines.length - 1, Math.floor(rand() * lines.length))]!;
}
