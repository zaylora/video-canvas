/** 打字机各阶段的时长（毫秒） */
export interface TypewriterTiming {
  /** 开场前的等待，只在页面刚打开时等一次 */
  delay: number;
  /** 打字时相邻两个字的间隔 */
  typeInterval: number;
  /** 删除时相邻两个字的间隔，比打字快 */
  deleteInterval: number;
  /** 一个词打完后停留多久，再换下一个 */
  hold: number;
  /** 词删光后空着多久，再打下一个词 */
  pause: number;
}

/** 某一时刻标题该显示成什么样 */
export interface TypewriterFrame {
  /** 前缀显示几个字 */
  prefix: number;
  /** 当前是第几个词 */
  word: number;
  /** 当前词显示几个字 */
  count: number;
  /** 词后面的结尾标点是否已出现：词一出现它就跟着出现，之后一直都在 */
  ended: boolean;
  /** 正在打字或删字：光标常亮；停留和空档时为 false，光标闪烁 */
  busy: boolean;
  /** 最后一个词打完了，动画停下，不再变化 */
  done: boolean;
}

const len = (text: string) => [...text].length;

/**
 * 标题打字机在某一时刻的画面。开场把「前缀 + 第一个词 + 结尾标点」整句打出来；
 * 之后依次换词：停留 → 删掉当前词 → 空档 → 打下一个词，前缀和结尾标点始终不动；
 * 最后一个词打完就停下（done），不再循环。
 * @param elapsed 从开始计时过去的毫秒数
 * @param prefixLength 前缀字数
 * @param words 依次出现的词
 * @param timing 各阶段时长
 * @returns 当前画面；没有词时只打前缀
 */
export const typewriterFrame = (
  elapsed: number,
  prefixLength: number,
  words: string[],
  timing: TypewriterTiming,
): TypewriterFrame => {
  const { delay, typeInterval, deleteInterval, hold, pause } = timing;
  const sizes = words.map(len);
  const introLength = prefixLength + (sizes[0] ?? 0);
  const idle = { prefix: 0, word: 0, count: 0, ended: false, busy: false, done: false };

  if (elapsed < delay) return idle;

  const introTyped = Math.floor((elapsed - delay) / typeInterval) + 1;
  if (introTyped < introLength) {
    return {
      ...idle,
      prefix: Math.min(introTyped, prefixLength),
      count: Math.max(0, introTyped - prefixLength),
      busy: true,
    };
  }
  if (sizes.length === 0) return { ...idle, prefix: prefixLength };

  const introEnd = delay + introLength * typeInterval;
  const base = { ...idle, prefix: prefixLength, ended: true };
  if (elapsed < introEnd) return { ...base, count: sizes[0], busy: true };

  let local = elapsed - introEnd;
  /** 第 i 步：词 i 停留 → 删光 → 空档 → 打词 i+1 */
  for (let i = 0; i < sizes.length - 1; i++) {
    const deleteEnd = hold + sizes[i] * deleteInterval;
    const pauseEnd = deleteEnd + pause;
    const step = pauseEnd + sizes[i + 1] * typeInterval;
    if (local >= step) {
      local -= step;
      continue;
    }
    if (local < hold) return { ...base, word: i, count: sizes[i] };
    if (local < deleteEnd) {
      const count = sizes[i] - Math.floor((local - hold) / deleteInterval) - 1;
      return { ...base, word: i, count, busy: true };
    }
    if (local < pauseEnd) return { ...base, word: i };
    const count = Math.floor((local - pauseEnd) / typeInterval) + 1;
    return { ...base, word: i + 1, count, busy: true };
  }
  const last = sizes.length - 1;
  return { ...base, word: last, count: sizes[last], done: true };
};
