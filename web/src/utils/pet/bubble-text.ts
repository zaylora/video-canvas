/** 气泡最多显示的字数（两行左右） */
export const BUBBLE_MAX_CHARS = 40;

/** 换行压成空格，超过 40 个字（按码点算）用省略号截断 */
export function clampBubbleText(text: string, max: number = BUBBLE_MAX_CHARS): string {
  const chars = Array.from(text.replace(/\s*\n\s*/g, " "));
  return chars.length <= max ? chars.join("") : chars.slice(0, max).join("") + "…";
}
