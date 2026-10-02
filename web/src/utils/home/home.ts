/**
 * 按小时给问候语：5–11 早上好，11–13 中午好，13–18 下午好，其余晚上好
 * @param hour 本地时间的小时，0–23
 * @returns 问候语，不带标点
 */
export const greeting = (hour: number) => {
  if (hour >= 5 && hour < 11) return "早上好";
  if (hour >= 11 && hour < 13) return "中午好";
  if (hour >= 13 && hour < 18) return "下午好";
  return "晚上好";
};

/** 节点线框封面的布局数量，和 components/home/canvas-cover 里的布局一一对应 */
export const COVER_LAYOUT_COUNT = 3;

/**
 * 没有封面的画布用哪种线框布局：按 id 做稳定哈希，同一张画布每次看到的都一样
 * @param id 画布 ID
 * @returns 0 到 COVER_LAYOUT_COUNT - 1 之间的下标
 */
export const coverLayoutOf = (id: string) => {
  let hash = 0;
  for (const char of id) hash = (hash * 31 + char.charCodeAt(0)) | 0;
  return Math.abs(hash) % COVER_LAYOUT_COUNT;
};

/**
 * 画布卡上的更新时间：当天显示「今天 HH:mm」，其余显示「YYYY-MM-DD HH:mm」；解析失败原样返回
 * @param value 后端给的时间串
 * @param now 当前时间，测试时注入
 * @returns 展示文案
 */
export const formatCanvasTime = (value: string, now: Date = new Date()) => {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const date = new Date(time);
  const pad = (n: number) => String(n).padStart(2, "0");
  const clock = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  if (date.toDateString() === now.toDateString()) return `今天 ${clock}`;
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${clock}`;
};
