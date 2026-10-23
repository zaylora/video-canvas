/**
 * 消息流里「历史」和「实时新增」的区分：历史（首次加载的、往上翻补来的）直接出现，不播入场动画；
 * 只有实时新增的消息淡入上浮。
 *
 * 靠 key 判断：消息流只会在前面补更早的一页、在末尾追加新消息，所以排在第一个已知 key 前面的都是补来的历史。
 * 返回新集合，不改入参。
 * @param known 上次的历史 key 集合；第一次传 null，此时已有的全算历史
 * @param keys 当前时间线各项的 key，按显示顺序
 */
export function trackHistoryKeys(known: ReadonlySet<string> | null, keys: readonly string[]) {
  if (known === null) return new Set(keys);
  const next = new Set(known);
  const firstKnown = keys.findIndex((key) => known.has(key));
  // 一个已知的都对不上：整条时间线被换掉了，不是实时新增，别让它们全飞进来
  const end = firstKnown === -1 ? keys.length : firstKnown;
  for (let i = 0; i < end; i++) next.add(keys[i]);
  return next;
}

/**
 * 在列表前面补了内容后新的滚动位置：内容在上面变高了多少，就往下滚多少，视线停在原来看的那条上。
 * 没变高（或变矮）不动。
 */
export const anchoredScrollTop = (top: number, prevHeight: number, nextHeight: number) =>
  top + Math.max(0, nextHeight - prevHeight);
