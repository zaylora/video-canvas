/** 页面滚动用到的浏览器能力，测试里换成假的 */
export type ScrollEnv = {
  /** 当前滚动位置（px） */
  scrollY: () => number;
  scrollTo: (options: ScrollToOptions) => void;
  /** 下一帧执行 */
  nextFrame: (callback: () => void) => void;
  /** 当前时间（毫秒） */
  now: () => number;
};

/** 滚动被打断（用户自己又滚了）时最多等这么久就照样切换（毫秒） */
const MAX_WAIT_MS = 1000;

/** 离顶部这么近就算到顶（px），平滑滚动最后一帧可能停在小数位置 */
const TOP_EPSILON = 1;

const windowEnv = (): ScrollEnv => ({
  scrollY: () => window.scrollY,
  scrollTo: (options) => window.scrollTo(options),
  nextFrame: (callback) => window.requestAnimationFrame(() => callback()),
  now: () => performance.now(),
});

/**
 * 「先平滑滚回顶部，到顶再切换内容」：换了类型或筛选，列表是另一批内容，停在原来的滚动位置没有意义。
 * 必须先滚后切：一切换列表就变成矮矮的骨架屏，页面变短，浏览器会直接把滚动位置截到顶部，看起来是跳上去的。
 * 已经在顶部就直接切；连着点了几次只切最后一次；滚动迟迟到不了顶（被打断）时超时照样切。
 * @param env 浏览器能力，缺省用 window
 * @returns run(apply)：滚到顶后执行 apply
 */
export function createScrollTopRunner(env?: ScrollEnv) {
  /** 最近一次的序号：旧的等待发现自己不是最新的就放弃 */
  let latest = 0;
  return (apply: () => void) => {
    const scroll = env ?? windowEnv();
    const token = ++latest;
    if (scroll.scrollY() <= TOP_EPSILON) {
      apply();
      return;
    }
    scroll.scrollTo({ top: 0, behavior: "smooth" });
    const started = scroll.now();
    const wait = () => {
      if (token !== latest) return;
      if (scroll.scrollY() <= TOP_EPSILON || scroll.now() - started >= MAX_WAIT_MS) apply();
      else scroll.nextFrame(wait);
    };
    scroll.nextFrame(wait);
  };
}
