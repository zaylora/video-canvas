import type { KeyEase } from "@/types";

type Keyed = { t: number; ease: KeyEase };

/** 关键帧时间精确到 0.01 秒 */
export const roundKeyTime = (t: number) => Math.round(Math.max(t, 0) * 100) / 100;

const sameTime = (a: number, b: number) => Math.abs(roundKeyTime(a) - roundKeyTime(b)) < 1e-9;

/** 记录关键帧：同一时刻已有就覆盖，结果按时间升序，不改动传入的数组 */
export function upsertKey<K extends Keyed>(keys: K[], key: K): K[] {
  const next = { ...key, t: roundKeyTime(key.t) };
  return [...keys.filter((k) => !sameTime(k.t, next.t)), next].sort((a, b) => a.t - b.t);
}

/** 删掉 t 时刻的关键帧 */
export const removeKey = <K extends Keyed>(keys: K[], t: number): K[] =>
  keys.filter((k) => !sameTime(k.t, t));

/** 把 from 时刻的关键帧挪到 to（不小于 0），落点已有帧则替换 */
export function moveKey<K extends Keyed>(keys: K[], from: number, to: number): K[] {
  const moving = keys.find((k) => sameTime(k.t, from));
  if (!moving) return keys;
  return upsertKey(removeKey(keys, from), { ...moving, t: to });
}

/** 改 t 时刻关键帧到下一帧的插值方式 */
export const setKeyEase = <K extends Keyed>(keys: K[], t: number, ease: KeyEase): K[] =>
  keys.map((k) => (sameTime(k.t, t) ? { ...k, ease } : k));
