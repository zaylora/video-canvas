import type { Round, Script } from "@/api/marketing/type";

/**
 * 营销创作台（设计 docs/design/营销工作室/创作台.md）的纯逻辑：
 * 选中项（原片 / 某份脚本）的默认与回落、镜头定位、时长展示。
 */

/** 左栏选中项：原片用 "ref"，脚本用脚本 ID */
export type WorkbenchSelection = "ref" | (string & {});

/** 原片在左栏的固定键 */
export const REF_SELECTION = "ref";

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/**
 * 时间 t 落在第几个镜头：返回第一个 t < end 的镜头；恰好在边界属于下一镜，超出末尾返回最后一镜。
 * @returns 镜头下标；没有镜头返回 -1
 */
export function shotIndexAt(shots: readonly { start: number; end: number }[], t: number): number {
  if (shots.length === 0) return -1;
  const index = shots.findIndex((shot) => t < shot.end);
  return index === -1 ? shots.length - 1 : index;
}

/** 脚本时长：最后一镜的终点 */
export function scriptDuration(script: Pick<Script, "shots">): number {
  return script.shots.at(-1)?.end ?? 0;
}

/** 左栏的顺序：原片（有参考时）在前，脚本按后端顺序 */
export function selectionKeys(round: Round): WorkbenchSelection[] {
  return [...(round.refVideo ? [REF_SELECTION] : []), ...round.scripts.map((s) => s.id)];
}

/**
 * 进入一轮时默认选中：第一份已出片的脚本 → 原片 → 第一份脚本。
 * @returns 选中项；这一轮什么都没有（失败 / 取消且无参考）返回 null
 */
export function defaultSelection(round: Round): WorkbenchSelection | null {
  const done = round.scripts.find((script) => script.status === "done");
  if (done) return done.id;
  if (round.refVideo) return REF_SELECTION;
  return round.scripts[0]?.id ?? null;
}

/** 当前选中项还在这一轮里就保留，否则回落到默认（轮次切换、脚本被替换时用） */
export function resolveSelection(
  round: Round,
  current: WorkbenchSelection | null,
): WorkbenchSelection | null {
  if (current && selectionKeys(round).includes(current)) return current;
  return defaultSelection(round);
}

/** ↑↓ 切换左栏项，到头停住 */
export function nextSelection(
  round: Round,
  current: WorkbenchSelection | null,
  step: 1 | -1,
): WorkbenchSelection | null {
  const keys = selectionKeys(round);
  if (keys.length === 0) return null;
  const index = current ? keys.indexOf(current) : -1;
  if (index === -1) return keys[0];
  return keys[clamp(index + step, 0, keys.length - 1)];
}

/** 时长展示：最多一位小数，整数不带 .0（14.833 → 14.8s，15 → 15s） */
export function formatSeconds(seconds: number): string {
  return `${Math.round(seconds * 10) / 10}s`;
}
