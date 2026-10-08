/** 单条视频超过这个大小（字节）就提示「首屏会变慢」 */
export const HEAVY_VIDEO_BYTES = 15 * 1024 * 1024;

/** 每条作品播放秒数的合法范围，和后端一致 */
export const CLIP_SECONDS_MIN = 4;
export const CLIP_SECONDS_MAX = 15;

/** 作品提示词的最大字数，和后端一致 */
export const PROMPT_MAX = 80;

/**
 * 轮播里的下一条（到头回到第一条）
 * @param index 当前下标
 * @param count 作品数量
 * @returns 下一条的下标；没有作品时为 0
 */
export const nextIndex = (index: number, count: number) => (count <= 0 ? 0 : (index + 1) % count);

/**
 * 把下标收回合法范围：作品被删、被禁用后当前下标可能越界
 * @param index 想要的下标
 * @param count 作品数量
 * @returns 落在 0 到 count - 1 之间的下标；没有作品时为 0
 */
export const clampIndex = (index: number, count: number) =>
  count <= 0 ? 0 : Math.min(Math.max(index, 0), count - 1);

/**
 * 要不要真的加载并播放视频：减少动态效果时只显示封面；
 * 浏览器开了省流量并且后台勾了「省流量时只显示封面」时也只显示封面。
 * @param reducedMotion 系统是否开启「减少动态效果」
 * @param saveData 浏览器是否开启省流量
 * @param posterOnSaveData 后台「省流量时只显示封面」开关
 */
export const shouldPlayVideo = (
  reducedMotion: boolean,
  saveData: boolean,
  posterOnSaveData: boolean,
) => !reducedMotion && !(saveData && posterOnSaveData);

/**
 * 要不要自动切下一条：减少动态效果时不自动切换，只能手动点进度段；只有一条时没有可切的
 * @param reducedMotion 系统是否开启「减少动态效果」
 * @param count 作品数量
 */
export const shouldAutoAdvance = (reducedMotion: boolean, count: number) =>
  !reducedMotion && count > 1;

/**
 * 没有封面或封面加载失败时，按作品 ID 稳定地算一个渐变色相
 * @param id 作品 ID
 * @returns 0–359 的色相
 */
export const fallbackHue = (id: string) => {
  let hash = 0;
  for (const char of id) hash = (hash * 31 + char.charCodeAt(0)) | 0;
  return Math.abs(hash) % 360;
};

/** 后台列表给作品标的体积 / 画幅提示 */
export type ShowcaseWarning = "heavy" | "portrait";

/**
 * 后台给作品的提示：体积太大会拖慢首屏，竖屏素材在横屏背景里会被裁切。只提示，不阻止。
 * @param byteSize 视频字节数，0 表示未知
 * @param width 视频宽度，0 表示未知
 * @param height 视频高度，0 表示未知
 * @returns 命中的提示，按「体积 → 画幅」的顺序
 */
export const showcaseWarnings = (byteSize: number, width: number, height: number) => {
  const list: ShowcaseWarning[] = [];
  if (byteSize > HEAVY_VIDEO_BYTES) list.push("heavy");
  if (width > 0 && height > width) list.push("portrait");
  return list;
};

/**
 * 校验轮播时长输入
 * @param seconds 秒数
 * @returns 合法返回空串，否则返回错误提示
 */
export const validateClipSeconds = (seconds: number) =>
  Number.isInteger(seconds) && seconds >= CLIP_SECONDS_MIN && seconds <= CLIP_SECONDS_MAX
    ? ""
    : `播放时长必须是 ${CLIP_SECONDS_MIN} 到 ${CLIP_SECONDS_MAX} 之间的整数秒`;
