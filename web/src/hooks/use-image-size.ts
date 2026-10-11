import { useEffect, useState } from "react";

/** 已经量过的图片真实像素，按地址缓存：重复进入扩图不用再量一遍 */
const measured = new Map<string, { w: number; h: number }>();

/**
 * 量出图片的真实像素（宽高，不是画布上显示的大小）。量到之前返回 null。
 * 扩图用它换算输出尺寸角标。
 * @param src 图片地址；没有就不量
 */
export function useImageSize(src: string | null | undefined): { w: number; h: number } | null {
  const [state, setState] = useState<{ src: string; size: { w: number; h: number } } | null>(null);

  useEffect(() => {
    if (!src || measured.has(src)) return;
    let cancelled = false;
    const image = new Image();
    image.onload = () => {
      if (image.naturalWidth <= 0 || image.naturalHeight <= 0) return;
      const size = { w: image.naturalWidth, h: image.naturalHeight };
      measured.set(src, size);
      if (!cancelled) setState({ src, size });
    };
    image.src = src;
    return () => {
      cancelled = true;
      image.onload = null;
    };
  }, [src]);

  if (!src) return null;
  return measured.get(src) ?? (state?.src === src ? state.size : null);
}
