/** 时间戳 → 本地时间；解析失败原样返回 */
export const formatTime = (value: string | null | undefined) => {
  if (!value) return "-";
  const time = Date.parse(value);
  return Number.isNaN(time) ? value : new Date(time).toLocaleString();
};

/** 时间戳 → “09-28 14:20”（设计稿里列表与详情用的短格式）；解析失败原样返回 */
export const formatShortTime = (value: string | null | undefined) => {
  if (!value) return "-";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const date = new Date(time);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
};
