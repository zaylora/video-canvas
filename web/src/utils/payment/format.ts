/**
 * 把「分」格式化成带 ¥ 的金额：整元不带小数（¥100），有零头保留两位（¥100.50），超过千元带千分位。
 * 全程用整数分运算，不经过浮点数，199 分永远是 ¥1.99。
 * @param fen 金额，单位分
 * @returns 展示用的金额文本
 */
export function formatYuan(fen: number): string {
  const yuan = Math.trunc(fen / 100);
  const cents = Math.abs(fen % 100);
  const whole = yuan.toLocaleString("en-US");
  return cents === 0 ? `¥${whole}` : `¥${whole}.${String(cents).padStart(2, "0")}`;
}

/**
 * 积分数量加千分位。
 * @param credits 积分数量
 * @returns 展示用的文本，如 10,000
 */
export function formatCredits(credits: number): string {
  return credits.toLocaleString("en-US");
}
