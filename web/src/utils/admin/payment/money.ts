/**
 * 把用户输入的「元」精确换算成「分」。全程字符串与整数运算，不经过浮点数，
 * 所以 19.9 永远是 1990 分（19.9 * 100 在浮点里是 1989.9999999999998）。
 * 只接受非负数字，最多两位小数；空串、负数、科学计数法、千分位、超过两位小数都视为非法。
 * @param input 输入框里的文本，如 "19.9"、"100"
 * @returns 金额（分）；输入非法或超出安全整数范围时为 null
 */
export function parseYuanToFen(input: string): number | null {
  const text = input.trim();
  if (!/^\d+(\.\d{1,2})?$/.test(text)) return null;
  const [whole, fraction = ""] = text.split(".");
  const fen = Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
  return Number.isSafeInteger(fen) ? fen : null;
}

/**
 * 把「分」转成输入框里的「元」文本，去掉多余的小数零：1990 → "19.9"，10000 → "100"，105 → "1.05"。
 * @param fen 金额，单位分
 * @returns 可以直接放进输入框、且能被 parseYuanToFen 还原的文本
 */
export function fenToYuanInput(fen: number): string {
  const whole = Math.trunc(fen / 100);
  const cents = Math.abs(fen % 100);
  if (cents === 0) return String(whole);
  return `${whole}.${String(cents).padStart(2, "0").replace(/0$/, "")}`;
}

/**
 * 严格解析非负整数：只接受纯数字（不接受小数、符号、空格内的字符、科学计数法）。
 * @param input 输入框里的文本
 * @returns 整数；非法或超出安全整数范围时为 null
 */
export function parseNonNegativeInt(input: string): number | null {
  const text = input.trim();
  if (!/^\d+$/.test(text)) return null;
  const value = Number(text);
  return Number.isSafeInteger(value) ? value : null;
}
