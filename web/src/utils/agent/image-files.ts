/** 一次粘贴 / 拖入最多附几张图：每张都会在画布上落成一个节点，太多会把画布铺满 */
export const MAX_ATTACH_IMAGES = 9;

/**
 * 从粘贴或拖入的文件里挑出图片：保持原顺序，超过上限的截掉。
 * 没有图片时 images 为空，调用方据此放行默认行为（文字照常粘进输入框）。
 * @returns images 要上传的图片；skipped 不是图片被忽略的个数；clipped 超过上限没放进去的张数
 */
export function pickImageFiles(files: ArrayLike<File> | null | undefined): {
  images: File[];
  skipped: number;
  clipped: number;
} {
  const all = Array.from(files ?? []);
  const images = all.filter((f) => f.type.startsWith("image/"));
  return {
    images: images.slice(0, MAX_ATTACH_IMAGES),
    skipped: all.length - images.length,
    clipped: Math.max(0, images.length - MAX_ATTACH_IMAGES),
  };
}
