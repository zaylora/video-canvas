/** 一个引用能不能弹放大预览，只看这几个字段 */
export type PeekTarget = {
  /** 源节点种类（image / video / audio / script 等）；没有 mediaType 时按它判断 */
  kind: string;
  /** 素材地址对应的媒体类型，有就以它为准 */
  mediaType?: string;
  /** 素材地址；文本节点、还没产出的节点、上传中的没有 */
  src?: string | null;
  /** 引用已断开（源节点被删或连线已断）：断开的不弹 */
  gone?: boolean;
};

/** 一个像素尺寸 */
export type PeekSize = {
  /** 宽，px */
  width: number;
  /** 高，px */
  height: number;
};

/** DOMRect 里这里用到的字段，测试里不必造真的 DOMRect */
export type PeekRect = {
  /** 左边，视口坐标 */
  left: number;
  /** 上边，视口坐标 */
  top: number;
  /** 右边，视口坐标 */
  right: number;
  /** 下边，视口坐标 */
  bottom: number;
  /** 宽 */
  width: number;
  /** 高 */
  height: number;
};

/** 预览相对目标摆在哪一侧 */
export type PeekSide = "above" | "below";

/** 预览的位置：固定定位的左上角（视口坐标），以及摆在目标的哪一侧 */
export type PeekPlacement = {
  /** 预览左边，视口坐标 */
  left: number;
  /** 预览上边，视口坐标 */
  top: number;
  /** 在目标上方还是下方 */
  side: PeekSide;
};

/** 预览的最大框，和首页参考图的悬停放大一致 */
export const PEEK_MAX: PeekSize = { width: 256, height: 288 };

/** 预览与目标的间距，px */
export const PEEK_GAP = 14;

/** 预览与视口边缘至少留的距离，px */
export const PEEK_MARGIN = 8;

/**
 * 预览里要放的是图片还是视频；文本、音频、已断开、没有地址的不弹，返回 null。
 * 媒体类型取 mediaType，没有就取 kind，和缩略图的取法一致。
 */
export function peekMedia(target: PeekTarget): "image" | "video" | null {
  if (target.gone || !target.src) return null;
  const media = target.mediaType ?? target.kind;
  return media === "image" || media === "video" ? media : null;
}

/** 这个引用要不要弹放大预览：图片、视频有地址才弹 */
export function canPeek(target: PeekTarget): boolean {
  return peekMedia(target) !== null;
}

/**
 * 预览实际占的大小：原始尺寸按比例缩进最大框，小图不放大。
 * 还不知道原始尺寸时（图还没加载）按最大框估，摆位时偏保守。
 * @param natural 图片或视频的原始尺寸
 * @param max 最大框，默认和首页一致
 */
export function peekSize(natural: PeekSize | null, max: PeekSize = PEEK_MAX): PeekSize {
  if (!natural || natural.width <= 0 || natural.height <= 0) return { ...max };
  const scale = Math.min(1, max.width / natural.width, max.height / natural.height);
  return { width: natural.width * scale, height: natural.height * scale };
}

/**
 * 预览摆在哪：默认在目标正上方 gap 处、水平居中；上方放不下（离视口顶不足 margin）就翻到目标下方；
 * 左右钳在视口内，视口比预览还窄时保证左边不出屏幕。
 * @param anchor 目标在视口里的矩形
 * @param size 预览的大小
 * @param viewport 视口大小
 * @param gap 与目标的间距
 * @param margin 与视口边缘至少留的距离
 */
export function peekPlacement({
  anchor,
  size,
  viewport,
  gap = PEEK_GAP,
  margin = PEEK_MARGIN,
}: {
  anchor: PeekRect;
  size: PeekSize;
  viewport: PeekSize;
  gap?: number;
  margin?: number;
}): PeekPlacement {
  const above = anchor.top - gap - size.height;
  const side: PeekSide = above < margin ? "below" : "above";
  const top = side === "above" ? above : anchor.bottom + gap;
  const centered = anchor.left + anchor.width / 2 - size.width / 2;
  const left = Math.max(margin, Math.min(centered, viewport.width - size.width - margin));
  return { left, top, side };
}
