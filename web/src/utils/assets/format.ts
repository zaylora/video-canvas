/**
 * 生成历史卡片上的日期：今年的写「10月14日」，往年带年份「2025年10月14日」。
 * @param value ISO 时间串
 * @param now 当前时间，测试时注入
 * @returns 本地时区的日期；解析不了时原样返回
 */
export function formatAssetDay(value: string, now: Date = new Date()): string {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const date = new Date(time);
  const day = `${date.getMonth() + 1}月${date.getDate()}日`;
  return date.getFullYear() === now.getFullYear() ? day : `${date.getFullYear()}年${day}`;
}
