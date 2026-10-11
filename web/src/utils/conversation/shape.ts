import { parseAspect } from "@/utils/canvas/node-frame";

/** 结果格子的画幅档位：只决定格子的高度，宽度由宽高比算出 */
export type CellShape = "square" | "wide" | "tall";

/** 宽高比和 1 相差不到这个比例就算方形，免得 1024×1008 这类被画成横图 */
const SQUARE_TOLERANCE = 0.02;

/**
 * 由宽高比推出格子画幅，用来挑格子的高度档位。
 * @param aspect 宽 / 高；无效值按横版
 * @returns 画幅
 */
export function shapeOfAspect(aspect: number): CellShape {
  if (!Number.isFinite(aspect) || aspect <= 0) return "wide";
  if (Math.abs(aspect - 1) < SQUARE_TOLERANCE) return "square";
  return aspect > 1 ? "wide" : "tall";
}

/**
 * 记录输入里的比例参数：不同模型叫法不同（aspect_ratio / ratio），都认。
 * @param input 记录的输入快照
 * @returns 比例参数值；没有为 undefined
 */
export const ratioOf = (input: Record<string, unknown>): unknown =>
  input.aspect_ratio ?? input.ratio;

/**
 * 格子此刻该用的宽高比：量到的成片真实比例优先，其次是用户选的比例（和画布节点同一套解析）。
 * 生成中、失败、成功各阶段都用它画框，框的比例才不会在加载前后变。
 * @param input 记录的输入快照
 * @param measured 成片实际的宽 / 高；还没量到为 null
 * @returns 宽 / 高；Auto 或写法不认识且没量到成片时为 null，由调用方按默认横版画
 */
export function cellAspect(input: Record<string, unknown>, measured: number | null): number | null {
  return measured ?? parseAspect(ratioOf(input));
}
