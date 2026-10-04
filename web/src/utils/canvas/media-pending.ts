/**
 * 节点内容（图片缩略图 / 原图 / 视频封面）加载占位的纯逻辑
 * （设计稿 docs/design/画布UI设计 第 6.12 节）。不碰 React 和 DOM，方便单测。
 */

/** 单个图层的请求进度 */
export type MediaLoadState = "pending" | "loaded" | "failed";

/**
 * 是否还该显示灰色加载占位。
 * 任意一层已经加载就是有内容了；没有任何一层还在等（全部失败）则交给破图占位。
 * @param layers 此刻实际在渲染的图层状态，没渲染的图层不要传
 */
export function isMediaPending(layers: readonly MediaLoadState[]): boolean {
  if (layers.includes("loaded")) return false;
  return layers.includes("pending");
}
