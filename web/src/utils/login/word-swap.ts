/**
 * 登录页标题里轮换的词：每过一个间隔换下一个，到头回到第一个。
 * @param elapsed 从开始轮换算起的毫秒数
 * @param count 词的个数
 * @param interval 每个词停留的毫秒数
 */
export function wordIndexAt(elapsed: number, count: number, interval: number) {
  if (count <= 1 || interval <= 0) return 0;
  return Math.floor(elapsed / interval) % count;
}

/**
 * 登录卡片顶部带过来的想法：用户在胶囊里写了想法（或点了「做同款」），登录后会带到创作页。
 * @param idea 胶囊里的文字
 * @returns 提示文案；没写时为 null，不显示
 */
export function carryLabel(idea: string) {
  const text = idea.trim();
  return text ? `登录后继续：${text}` : null;
}
