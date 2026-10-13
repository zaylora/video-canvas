/** 峰值低于它算静音，波形里画成圆点而不是竖条 */
export const SILENT_PEAK = 0.03;

/**
 * 把整段采样均分成 bars 份，每份取绝对值最大的采样，再按全局最大值归一化。
 * 为什么整段均分：节点里的波形是静止铺满的，播放头在上面走，不是滚动的。
 * @param samples 单声道采样（-1 ~ 1）
 * @param bars 要几根竖条
 * @returns 长度为 bars 的 0~1 峰值；没有采样或 bars 无效时为空数组，全静音时全为 0
 */
export function computePeaks(samples: Float32Array, bars: number): number[] {
  if (samples.length === 0 || !Number.isInteger(bars) || bars <= 0) return [];
  const peaks = Array.from({ length: bars }, (_, i) => {
    const from = Math.floor((i * samples.length) / bars);
    const to = Math.max(from + 1, Math.floor(((i + 1) * samples.length) / bars));
    let peak = 0;
    for (let j = from; j < to && j < samples.length; j++) {
      peak = Math.max(peak, Math.abs(samples[j]));
    }
    return peak;
  });
  const max = Math.max(...peaks);
  return max > 0 ? peaks.map((peak) => peak / max) : peaks;
}

/**
 * 把秒数写成 `时:分:秒`（各两位），并拆成「前面的 0」和「有效位」两段：
 * 前一段用弱化色，后一段用正常色，一眼看出走到哪了。
 * @param seconds 秒；无效或负数按 0，小数向下取整
 * @returns dim 是第一个非零数字之前的部分，bright 是其后；全 0 时 bright 只剩最后一个 0
 */
export function clockParts(seconds: number): { dim: string; bright: string } {
  const total = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0;
  const pad = (n: number) => String(n).padStart(2, "0");
  const text = [Math.floor(total / 3600), Math.floor(total / 60) % 60, total % 60]
    .map(pad)
    .join(":");
  const first = text.search(/[1-9]/);
  const cut = first === -1 ? text.length - 1 : first;
  return { dim: text.slice(0, cut), bright: text.slice(cut) };
}

/**
 * 波形上的位置换成时间。
 * @param position 0~1，播放头在波形上的相对位置；超出范围夹在两端
 * @param duration 总时长，秒
 * @returns 秒
 */
export function positionToTime(position: number, duration: number): number {
  return Math.min(1, Math.max(0, position)) * duration;
}

/**
 * 时间换成波形上的位置。
 * @param time 秒
 * @param duration 总时长，秒
 * @returns 0~1；总时长为 0 或无效时为 0
 */
export function timeToPosition(time: number, duration: number): number {
  if (!Number.isFinite(duration) || duration <= 0) return 0;
  return Math.min(1, Math.max(0, time / duration));
}
