/** 进度到这里就把工具条换成顶栏里的胶囊 */
export const COLLAPSE_AT = 0.9;

/**
 * 工具条随滚动收起的进度：工具条顶边碰到顶栏底边时为 0，整条滑过顶栏时为 1，按滚动距离线性增长。
 * 页面拿它直接驱动工具条的缩放和透明度（跟手），到 COLLAPSE_AT 时换成顶栏里的胶囊。
 * @param scrollY 当前滚动距离
 * @param top 工具条在文档里的顶边（不随滚动变）
 * @param height 工具条高度；还没布局（0）时不收
 * @param offset 顶栏高度，工具条滑到它下面才开始收
 */
export function collapseProgress({
  scrollY,
  top,
  height,
  offset,
}: {
  scrollY: number;
  top: number;
  height: number;
  offset: number;
}) {
  if (height <= 0) return 0;
  const progress = (scrollY - (top - offset)) / height;
  return Math.min(1, Math.max(0, progress));
}
