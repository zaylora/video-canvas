/**
 * 空状态占位图标：统一的实心剪影一家子。
 * lucide 那套是描边款，撑到占位框那么大就细得发虚，所以这里自己画。
 * 都按 24 的画布排版，尺寸和颜色交给外面的 className。
 */

type PlaceholderIconProps = {
  className?: string;
};

/** 山峦加一轮小日头，图片没出图时的占位 */
export function ImagePlaceholderIcon({ className }: PlaceholderIconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden
      className={className}
    >
      <circle cx="7.5" cy="8" r="2" />
      <path d="M14.2 8.6 21 18H9.6l4.6-9.4Z" />
      <path d="M6.6 12.4 10.5 18H3l3.6-5.6Z" />
    </svg>
  );
}

/** 摄影机，视频没生成时的占位 */
export function VideoPlaceholderIcon({ className }: PlaceholderIconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden
      className={className}
    >
      <path d="M3 7.2A2.2 2.2 0 0 1 5.2 5h9.6A2.2 2.2 0 0 1 17 7.2v9.6A2.2 2.2 0 0 1 14.8 19H5.2A2.2 2.2 0 0 1 3 16.8V7.2Z" />
      <path d="m19.4 9.3 1.6-1.1v7.6l-1.6-1.1a1.6 1.6 0 0 1-.7-1.3v-2.8c0-.5.3-1 .7-1.3Z" />
    </svg>
  );
}

/** 三条文字行，文本还没写时的占位 */
export function TextPlaceholderIcon({ className }: PlaceholderIconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden
      className={className}
    >
      <rect x="3" y="6" width="18" height="2.4" rx="1.2" />
      <rect x="3" y="10.8" width="18" height="2.4" rx="1.2" />
      <rect x="3" y="15.6" width="11" height="2.4" rx="1.2" />
    </svg>
  );
}

/** 声波，音频还没生成时的占位 */
export function AudioPlaceholderIcon({ className }: PlaceholderIconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden
      className={className}
    >
      <rect x="2.6" y="10" width="2.4" height="4" rx="1.2" />
      <rect x="7" y="7" width="2.4" height="10" rx="1.2" />
      <rect x="11.4" y="4.2" width="2.4" height="15.6" rx="1.2" />
      <rect x="15.8" y="7" width="2.4" height="10" rx="1.2" />
      <rect x="20.2" y="10" width="2.4" height="4" rx="1.2" />
    </svg>
  );
}
