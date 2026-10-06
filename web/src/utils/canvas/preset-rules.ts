import { PRESET_KINDS, type PresetKind } from "@/constants/presets";

/** 预设里提交时要展开成提示词的那部分，只有正文参与字数 */
type PresetText = { prompt: string };

/** 提示词里已经放了的一个预设 */
export type PresetPick = { kind: PresetKind; id: string };

/** 选了一个预设之后，编辑器该做什么 */
export type PresetPlan =
  /** 取消：删掉第 index 个预设 chip（按提示词里的出现顺序数） */
  | { type: "remove"; index: number }
  /** 原位替换第 index 个预设 chip */
  | { type: "replace"; index: number }
  /** 新插入：模板在最前面，风格和运镜在光标处 */
  | { type: "insert"; at: "start" | "cursor" }
  /** 不执行：从 chip 点进来选了提示词别处已有的那一条运镜，不提示 */
  | { type: "blocked" };

/**
 * 选中一个预设时的数量规则（设计稿 6.13）：
 * - 风格、模板各至多一个，再选同类的另一个原位替换；运镜可以同时有多个，但各不相同，每次只点一个。
 * - 再点已选的那个是取消。
 * - 从提示词里的 chip 点进来（target 是它在 selected 里的序号）：选另一个就替换这一个，
 *   选了别处已有的那条运镜则不执行。
 * 纯函数，编辑器按返回值去改文档。
 */
export function planPreset(
  selected: readonly PresetPick[],
  kind: PresetKind,
  id: string,
  target?: number,
): PresetPlan {
  const same = selected.findIndex((item) => item.kind === kind && item.id === id);
  if (target !== undefined) {
    if (selected[target]?.id === id) return { type: "remove", index: target };
    if (same >= 0) return { type: "blocked" };
    return { type: "replace", index: target };
  }
  if (same >= 0) return { type: "remove", index: same };
  if (!PRESET_KINDS[kind].multi) {
    const existing = selected.findIndex((item) => item.kind === kind);
    if (existing >= 0) return { type: "replace", index: existing };
  }
  return { type: "insert", at: kind === "tpl" ? "start" : "cursor" };
}

/**
 * 预设展开后的正文比模型提示词上限还长时，返回它的字数，否则返回 null。
 * 提交时按展开后的长度校验，预设本身超限的话选了也一定发不出去，所以选择器里先提示。
 */
export function presetOverflow(preset: PresetText, maxLength: number | undefined): number | null {
  if (!maxLength || maxLength <= 0) return null;
  const length = [...preset.prompt.trim()].length;
  return length > maxLength ? length : null;
}
