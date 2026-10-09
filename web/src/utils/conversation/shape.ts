/** 结果格子的画幅：决定格子的宽高比 */
export type CellShape = "square" | "wide" | "tall";

/**
 * 由记录输入里的比例参数（如「16:9」）推出格子画幅。
 * 比例是「Auto」或写法不认识时按横版画：多数模型的默认输出是横版。
 * @param ratio input 里的比例参数值
 * @returns 画幅
 */
export function shapeOf(ratio: unknown): CellShape {
  const match = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/.exec(String(ratio ?? ""));
  if (!match) return "wide";
  const width = Number(match[1]);
  const height = Number(match[2]);
  if (width === height) return "square";
  return width > height ? "wide" : "tall";
}

/**
 * 记录输入里的比例参数：不同模型叫法不同（aspect_ratio / ratio），都认。
 * @param input 记录的输入快照
 * @returns 比例参数值；没有为 undefined
 */
export const ratioOf = (input: Record<string, unknown>): unknown =>
  input.aspect_ratio ?? input.ratio;
