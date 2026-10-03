/** 停手多久才保存：打字、拖动、增删节点统一用这一个窗口 */
export const SAVE_IDLE_MS = 3000;

/** 连续编辑时最长等多久：从第一次改动算起，超过就不再往后推 */
export const SAVE_MAX_WAIT_MS = 30_000;

/**
 * keepalive 请求体的大小上限：浏览器规定约 64KB，留一点余量给请求头。
 * 超了就发普通请求，页面关掉时只能靠 beforeunload 的提示兜底。
 */
const KEEPALIVE_MAX_BYTES = 60 * 1024;

/**
 * 距离下一次保存还要等多久（毫秒）：停手窗口和最长等待取先到的那个。
 * 在途保存落地后补排期也用它——停手窗口早就过了就立即保存，没过只补差额，不会无条件立刻补发。
 */
export function nextSaveDelay({
  now,
  firstDirtyAt,
  lastChangeAt,
  idleMs = SAVE_IDLE_MS,
  maxWaitMs = SAVE_MAX_WAIT_MS,
}: {
  now: number;
  /** 这一批未保存改动里最早的一次 */
  firstDirtyAt: number;
  /** 最近一次改动 */
  lastChangeAt: number;
  idleMs?: number;
  maxWaitMs?: number;
}) {
  const byIdle = lastChangeAt + idleMs - now;
  const byMaxWait = firstDirtyAt + maxWaitMs - now;
  return Math.max(0, Math.min(byIdle, byMaxWait));
}

/** 这份内容能不能用 keepalive 发：按 UTF-8 字节数算，中文一个字占 3 字节 */
export function canKeepalive(payload: unknown) {
  return new Blob([JSON.stringify(payload)]).size <= KEEPALIVE_MAX_BYTES;
}
