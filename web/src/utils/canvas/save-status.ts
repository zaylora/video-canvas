/**
 * - saving：有改动在落盘，或刚写完还没静默够；连续编辑期间一直是它，不随每次写入来回闪
 * - error：云端重试仍失败
 * - conflict：别处改过了，等用户在弹窗里选怎么处理
 */
export type SaveStatus = "loading" | "saved" | "saving" | "error" | "conflict";

/** 最后一次本地写入后，静默多久才从「正在保存」切到「已保存」 */
export const STATUS_SETTLE_MS = 800;

/** 云端累计失败几次才变红：第一次失败静默，第一次自动重试仍失败才提示 */
export const CLOUD_FAIL_VISIBLE_AT = 2;

/**
 * 状态栏该显示什么。
 * - 已保存 → 正在保存：立即；正在保存 → 已保存：防抖
 * - 冲突立即显示；云端失败延后到第二次失败；云端上传本身不显示（本地草稿已兜底）
 * - fallbackBusy：没有本地草稿兜底时，云端有未同步内容就按正在保存显示
 */
export function displayStatus({
  conflict,
  cloudFailCount,
  localPending,
  lastLocalWriteAt,
  fallbackBusy,
  now,
  settleMs = STATUS_SETTLE_MS,
}: {
  conflict: boolean;
  cloudFailCount: number;
  /** 有还没写进草稿的改动 */
  localPending: boolean;
  /** 最近一次本地写入完成的时间；没写过是 null */
  lastLocalWriteAt: number | null;
  fallbackBusy: boolean;
  now: number;
  settleMs?: number;
}): SaveStatus {
  if (conflict) return "conflict";
  if (cloudFailCount >= CLOUD_FAIL_VISIBLE_AT) return "error";
  if (localPending || fallbackBusy) return "saving";
  if (lastLocalWriteAt !== null && now < lastLocalWriteAt + settleMs) return "saving";
  return "saved";
}

/** 还要等多久静默才满，用来排「切到已保存」的计时；还有待写入时不排，等写完再算 */
export function statusSettleDelay({
  now,
  lastLocalWriteAt,
  localPending,
  settleMs = STATUS_SETTLE_MS,
}: {
  now: number;
  lastLocalWriteAt: number | null;
  localPending: boolean;
  settleMs?: number;
}) {
  if (localPending || lastLocalWriteAt === null) return 0;
  return Math.max(0, lastLocalWriteAt + settleMs - now);
}
