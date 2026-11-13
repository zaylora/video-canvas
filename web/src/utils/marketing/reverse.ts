import type { Reverse, ReverseShot, Round } from "@/api/marketing/type";

import { isScriptBusy } from "./script";

/** 动作至少写几个字：只写一两个字的不保存（服务端另有「不只是名词」的校验） */
const ACTION_MIN_CHARS = 4;

/** 用户能改的四个字段是否相同 */
const sameEditable = (a: ReverseShot, b: ReverseShot): boolean =>
  a.action === b.action &&
  a.dialogue === b.dialogue &&
  a.productRole === b.productRole &&
  a.keyColors.join("\u0000") === b.keyColors.join("\u0000");

/** 两份拆解在用户可改字段上有没有差异 */
function editableDiffers(a: Reverse, b: Reverse): boolean {
  if (a.shots.length !== b.shots.length) return true;
  return a.shots.some((shot, index) => !sameEditable(shot, b.shots[index]));
}

/**
 * 「拆解有改动」：服务端说和当前跟拍版据以写成的拆解不同（reverseDirty），
 * 或本地草稿和服务端存的拆解不同。有改动才出现「按拆解重写跟拍版」。
 * @param round 轮次
 * @param draft 本地还没保存完的草稿；没有传 null
 */
export function isReverseDirty(round: Round, draft: Reverse | null): boolean {
  if (!round.reverse) return false;
  if (round.reverseDirty) return true;
  return draft ? editableDiffers(round.reverse, draft) : false;
}

/** 跟拍脚本（如果有） */
const followScript = (round: Round) => round.scripts.find((script) => script.kind === "follow");

/** 拆解表是否可编辑：跟拍版重写中或出片中时只读 */
export function reverseEditable(round: Round): boolean {
  const follow = followScript(round);
  return !(follow && isScriptBusy(follow.status));
}

/**
 * 「按拆解重写跟拍版」按钮的状态：跟拍版重写 / 出片中 → 没有改动 → 余额不足。
 * @param round 轮次
 * @param draft 本地草稿
 * @param available 可用积分；还没拿到为 null
 * @param cost 重写跟拍版的积分；还没算出来为 null
 */
export function refollowState(
  round: Round,
  draft: Reverse | null,
  available: number | null,
  cost: number | null,
): { disabled: boolean; reason: string } {
  if (!reverseEditable(round)) return { disabled: true, reason: "跟拍版正在生成或出片" };
  if (!isReverseDirty(round, draft)) return { disabled: true, reason: "拆解没有改动" };
  if (available !== null && cost !== null && available < cost) {
    return { disabled: true, reason: "余额不足" };
  }
  return { disabled: false, reason: "只重跑跟拍改写" };
}

/** 改某个镜头的字段，返回新拆解，不改原对象 */
export function patchReverseShot(
  reverse: Reverse,
  index: number,
  patch: Partial<Pick<ReverseShot, "action" | "dialogue" | "productRole" | "keyColors">>,
): Reverse {
  return {
    ...reverse,
    shots: reverse.shots.map((shot, i) => (i === index ? { ...shot, ...patch } : shot)),
  };
}

/** 动作能不能保存；返回不能的原因 */
export function validateAction(action: string): string | null {
  const text = action.trim();
  if (text.length < ACTION_MIN_CHARS)
    return "动作写得太短，拆解没保存（主体 + 动作，写到发力过程）";
  return null;
}

/** 拆解卡折叠时的一行摘要 */
export function reverseSummary(reverse: Reverse) {
  return {
    shotCount: reverse.shots.length,
    seconds: reverse.shots.length > 0 ? reverse.shots[reverse.shots.length - 1].end : 0,
    whyViral: reverse.overall.whyViral,
    brandCount: reverse.brands.length,
    claimCount: reverse.claims.length,
  };
}
