import type { Capabilities } from "@/api/model/type";
import { effectiveValue, paramEntries } from "@/utils/tasks/capabilities";

/**
 * 图片节点的预览画幅（设计稿 docs/design/画布UI设计 6.17「变更：原为固定 16:9」）：
 * 出了图就按图片自己的真实比例，没出图时跟着生成面板里选的比例，宽度不变、高度随比例。
 * 这里只算「宽 / 高」，怎么量出图片的真实比例、怎么写进节点，由节点组件负责。
 */

/** 没有任何比例信息时的画幅 */
export const DEFAULT_NODE_ASPECT = 16 / 9;

/** 极端比例的上下限：太窄、太宽的图把节点撑得过高或过扁，超出的部分仍然留白（宽 / 高） */
export const NODE_ASPECT_MIN = 1 / 2;
export const NODE_ASPECT_MAX = 2;

/** 把画幅限制在 1:2 到 2:1 */
export const clampNodeAspect = (aspect: number) =>
  Math.min(Math.max(aspect, NODE_ASPECT_MIN), NODE_ASPECT_MAX);

/** 「宽 分隔符 高」：16:9、16/9、16x9、1024×1024，分隔符前后可以有空格 */
const RATIO_TEXT = /^\s*(\d+(?:\.\d+)?)\s*[:：/xX×]\s*(\d+(?:\.\d+)?)\s*$/;

/**
 * 把比例文字读成宽 / 高。Auto、空、乱写、宽或高为 0 都读不出来，返回 undefined。
 */
export function parseRatio(value: unknown): number | undefined {
  if (typeof value !== "string") return undefined;
  const match = RATIO_TEXT.exec(value);
  if (!match) return undefined;
  const w = Number(match[1]);
  const h = Number(match[2]);
  return w > 0 && h > 0 ? w / h : undefined;
}

/** 参数名或标题里带这些字，才可能是「比例」参数 */
const RATIO_HINT = /aspect|ratio|size|比例|画幅|尺寸|宽高/i;

/**
 * 从模型的生成参数里找出「比例」当前选的是什么（宽 / 高）。
 * 认法：枚举参数，名字或标题带「比例 / 画幅 / 尺寸 / aspect / ratio / size」，
 * 并且选项里至少有一个读得出 宽:高（Auto 这类读不出的选项不算数）。
 * 取值：用户选的，没选就取默认值（没开放给用户的参数后端照默认值生成，也算数）；
 * 选的是 Auto 就没有比例，返回 undefined。
 */
export function ratioParamValue(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
): number | undefined {
  for (const field of paramEntries(caps)) {
    if (field.type !== "enum" || !field.options) continue;
    if (!RATIO_HINT.test(`${field.name} ${field.label}`)) continue;
    if (!field.options.some((option) => parseRatio(option) !== undefined)) continue;
    const ratio = parseRatio(effectiveValue(field, params, field.name));
    if (ratio !== undefined) return ratio;
  }
  return undefined;
}

/**
 * 图片节点此刻该用什么画幅：
 * - 出了图：图片自己的真实比例（已量出来的），还没量出来先用面板选的比例顶着；
 * - 还没出图（空、排队、生成中、失败）：跟着面板选的比例，面板没有比例时用已知的比例
 *   （比如扩图结果节点建出来时就知道），都没有才是 16:9。
 * 结果限制在 1:2 到 2:1。
 * @param args.done 节点现在有图
 * @param args.measured 已经量出来（或建节点时就知道）的图片比例，写在节点数据里
 */
export function imageNodeAspect(args: {
  done: boolean;
  measured?: number;
  caps: Capabilities | undefined;
  params: Record<string, unknown>;
}): number {
  const chosen = ratioParamValue(args.caps, args.params);
  const aspect = args.done
    ? (args.measured ?? chosen ?? DEFAULT_NODE_ASPECT)
    : (chosen ?? args.measured ?? DEFAULT_NODE_ASPECT);
  return clampNodeAspect(aspect);
}
