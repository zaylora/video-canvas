import type {
  FlagType,
  MethodInfo,
  Round,
  RoundSettings,
  Script,
  ScriptFlag,
  ScriptStatus,
} from "@/api/marketing/type";

/** 旁白问题标签的展示名 */
export const FLAG_NAME: Record<FlagType, string> = {
  forbidden: "禁用说法",
  claim: "背书无依据",
  brand: "第三方品牌",
};

const STATUS_LABEL: Record<ScriptStatus, string> = {
  draft: "待出片",
  rewriting: "重写中",
  rendering: "出片中",
  done: "已出片",
  failed: "出片失败",
};

/** 脚本状态的展示名 */
export const scriptStatusLabel = (status: ScriptStatus): string => STATUS_LABEL[status];

/** 脚本正在出片或重写：不能改旁白、不能再出片 */
export const isScriptBusy = (status: ScriptStatus): boolean =>
  status === "rendering" || status === "rewriting";

/** 有问题的镜头下标集合（旁白描红用） */
export const flaggedShots = (flags: readonly ScriptFlag[]): Set<number> =>
  new Set(flags.map((flag) => flag.shot));

/** 问题类型，按第一次出现的顺序去重（脚本卡底部的问题标签） */
export function flagSummary(flags: readonly ScriptFlag[]): FlagType[] {
  return [...new Set(flags.map((flag) => flag.type))];
}

/** 「旁白有问题：禁用说法「最」、背书无依据「销量第一」」：相同的问题合并 */
export function flagReason(flags: readonly ScriptFlag[]): string {
  const parts = [...new Set(flags.map((flag) => `${FLAG_NAME[flag.type]}「${flag.text}」`))];
  return `旁白有问题：${parts.join("、")}`;
}

/** 出片按钮的状态 */
export interface ScriptRenderState {
  /** 禁用 */
  disabled: boolean;
  /** 禁用原因，可用时为空串 */
  reason: string;
  /** 已出过片（或上次失败）：按钮叫「重出」 */
  again: boolean;
}

/**
 * 脚本卡「出片 / 重出」按钮能不能点：出片中 / 重写中 → 旁白有问题（改掉才能出片）→ 余额不足。
 * 余额还没拿到（null）不拦，后端 40001 兜底。
 * @param script 脚本
 * @param available 可用积分
 */
export function scriptRenderState(script: Script, available: number | null): ScriptRenderState {
  const again = script.status === "done" || script.status === "failed";
  if (isScriptBusy(script.status)) {
    return {
      disabled: true,
      reason: script.status === "rendering" ? "出片中" : "重写中",
      again,
    };
  }
  if (script.flags.length > 0) return { disabled: true, reason: flagReason(script.flags), again };
  if (available !== null && available < script.renderCredits) {
    return { disabled: true, reason: "余额不足", again };
  }
  return { disabled: false, reason: "", again };
}

/** 旁白相对脚本当前内容有没有改动（首尾空白不算），决定要不要保存 */
export function narrationsChanged(script: Script, narrations: readonly string[]): boolean {
  if (narrations.length !== script.shots.length) return true;
  return script.shots.some((shot, index) => shot.narration.trim() !== narrations[index].trim());
}

/** 「全部出片」按钮的状态 */
export interface RenderAllState {
  /** 是否显示：轮次写好了且可出片的不止一份 */
  show: boolean;
  /** 可出片的份数 */
  count: number;
  /** 合计积分 */
  credits: number;
  /** 禁用 */
  disabled: boolean;
  /** 禁用原因 */
  reason: string;
}

/**
 * 轮次头「全部出片」：只统计可出片的脚本（出片中、重写中、旁白有问题的不算，后端同样会跳过）。
 * @param round 轮次
 * @param available 可用积分
 */
export function renderAllState(round: Round, available: number | null): RenderAllState {
  const renderable = round.scripts.filter(
    (script) => !isScriptBusy(script.status) && script.flags.length === 0,
  );
  const credits = renderable.reduce((sum, script) => sum + script.renderCredits, 0);
  const show = round.status === "ready" && renderable.length > 1;
  const lack = available !== null && available < credits;
  return {
    show,
    count: renderable.length,
    credits,
    disabled: lack,
    reason: lack ? "余额不足" : "",
  };
}

/** 方法小字：内置方法 / 技能名 vN；技能停用退回内置的显示内置 */
export function methodLabel(method: MethodInfo | null | undefined): string {
  if (!method || method.source === "builtin" || method.fallback) return "内置方法";
  return `${method.skillName} v${method.version}`;
}

/**
 * 轮次的成片画幅（宽、高），画成片框用。画幅参数名取 aspect_ratio 或 ratio，
 * 取不到或格式不对按竖屏 9:16。
 */
export function roundAspect(settings: Pick<RoundSettings, "params">): [number, number] {
  const raw = settings.params.aspect_ratio ?? settings.params.ratio;
  const match = typeof raw === "string" ? /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/.exec(raw) : null;
  if (!match) return [9, 16];
  const [w, h] = [Number(match[1]), Number(match[2])];
  return w > 0 && h > 0 ? [w, h] : [9, 16];
}

/** 脚本卡成片区当前要播的那一条：只算出了成片的 take，选中的下标越界取最后一条 */
export function playableTakes(script: Script) {
  return script.takes.filter((take) => take.outputs.length > 0);
}
