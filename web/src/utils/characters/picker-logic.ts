import type { CharacterDto } from "@/api/character/type";

/**
 * 勾选或取消一张角色卡：多选时追加到末尾 / 移除；单选（重新选择已失效的节点）时只留这一张，再点同一张取消。
 * @param selected 当前已选角色卡 ID，按勾选顺序
 * @param id 被点的角色卡
 * @param single 是否单选
 */
export function toggleCharacter(selected: string[], id: string, single: boolean): string[] {
  if (selected.includes(id)) return selected.filter((item) => item !== id);
  return single ? [id] : [...selected, id];
}

/**
 * 按名称搜索角色卡：包含关键字即可，忽略大小写和首尾空格。
 * @param cards 全部角色卡
 * @param q 关键字；空串返回原数组
 */
export function filterCharacters(cards: CharacterDto[], q: string): CharacterDto[] {
  const keyword = q.trim().toLowerCase();
  if (!keyword) return cards;
  return cards.filter((card) => card.name.toLowerCase().includes(keyword));
}

/** 选择框底部确认按钮的文案与状态 */
export type CharacterPickerSummary = {
  /** 按钮文字：多选「放到画布 · N」，单选「替换」 */
  confirmLabel: string;
  /** 没选任何角色卡时禁用 */
  disabled: boolean;
};

/**
 * 选择框底部确认按钮。
 * @param selected 已选角色卡 ID
 * @param single 是否单选（替换已失效节点）
 */
export function characterPickerSummary(
  selected: string[],
  single: boolean,
): CharacterPickerSummary {
  const count = selected.length;
  if (single) return { confirmLabel: "替换", disabled: count === 0 };
  return { confirmLabel: count > 0 ? `放到画布 · ${count}` : "放到画布", disabled: count === 0 };
}
