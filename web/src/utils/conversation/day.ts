/**
 * 对话里记录的日期分组标题：「10月9日」，不是今年的带上年份「2025年10月9日」。
 * 按用户本地时区取日期，和用户看到的「今天」一致。
 * @param iso 记录创建时间（ISO 字符串）
 * @param now 当前时间，测试时传入
 * @returns 标题；时间无法解析时是空串
 */
export function dayTitle(iso: string, now: Date = new Date()): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const base = `${date.getMonth() + 1}月${date.getDate()}日`;
  return date.getFullYear() === now.getFullYear() ? base : `${date.getFullYear()}年${base}`;
}
