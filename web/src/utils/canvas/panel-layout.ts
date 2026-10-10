/**
 * 面板宽度（屏幕像素）：不随画布缩放变化，缩小、放大都保持这个大小。
 * 取 100% 缩放时默认节点宽 576 + 104，照参考图。
 */
export const PANEL_WIDTH = 680;
/** 离视口边缘至少留这么多 */
export const VIEWPORT_GUTTER = 12;

/**
 * 生成面板的宽度和水平位移（设计稿 6.4）：
 * 宽度固定 PANEL_WIDTH，视口放不下时夹到视口宽减两边留白；
 * 面板以节点中线为轴居中，超出视口的那部分用 shift 挪回来。
 * @param input 节点在屏幕上的宽度、左边缘 x，以及视口宽度（都是屏幕像素）
 */
export function panelLayout(input: { nodeWidth: number; nodeLeft: number; viewportWidth: number }) {
  const { nodeWidth, nodeLeft, viewportWidth } = input;
  const width = Math.min(PANEL_WIDTH, viewportWidth - VIEWPORT_GUTTER * 2);
  const left = nodeLeft + nodeWidth / 2 - width / 2;
  const right = left + width;
  const shift =
    left < VIEWPORT_GUTTER
      ? VIEWPORT_GUTTER - left
      : right > viewportWidth - VIEWPORT_GUTTER
        ? viewportWidth - VIEWPORT_GUTTER - right
        : 0;
  return { width, shift };
}
