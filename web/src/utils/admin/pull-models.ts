import type { ImportModelsResult, PulledModel } from "@/api/admin/ai/type";

/** 选择情况的统计 */
export type SelectionStats = {
  /** 上游返回的模型总数 */
  total: number;
  /** 已勾选数（含已存在的） */
  selected: number;
  /** 已勾选的新模型数：确认导入时真正会新建的 */
  newCount: number;
  /** 已勾选但已存在的模型数：导入时会跳过 */
  existingCount: number;
};

/**
 * 按模型 id 搜索（不区分大小写）。
 * @param items 上游返回的模型
 * @param query 搜索词，空白表示不过滤
 */
export function filterPulled(items: readonly PulledModel[], query: string): PulledModel[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...items];
  return items.filter((item) => item.id.toLowerCase().includes(q));
}

/**
 * 默认选择：全部勾选（已存在的也勾，导入时由后端跳过）。
 * @param items 上游返回的模型
 */
export function initialSelection(items: readonly PulledModel[]): Set<string> {
  return new Set(items.map((item) => item.id));
}

/**
 * 统计选择情况；只数清单里存在的 id。
 * @param items 上游返回的模型
 * @param selected 已勾选的 id
 */
export function selectionStats(
  items: readonly PulledModel[],
  selected: ReadonlySet<string>,
): SelectionStats {
  const picked = items.filter((item) => selected.has(item.id));
  const newCount = picked.filter((item) => !item.exists).length;
  return {
    total: items.length,
    selected: picked.length,
    newCount,
    existingCount: picked.length - newCount,
  };
}

/**
 * 批量全选 / 取消全选，只作用于当前搜索命中的项，其余项的勾选状态不变。
 * @param items 上游返回的模型
 * @param selected 当前已勾选的 id（不会被修改）
 * @param query 当前搜索词，空白表示作用于全部
 * @param checked true 全选，false 取消全选
 * @returns 新的已勾选集合
 */
export function setSelection(
  items: readonly PulledModel[],
  selected: ReadonlySet<string>,
  query: string,
  checked: boolean,
): Set<string> {
  const next = new Set(selected);
  for (const item of filterPulled(items, query)) {
    if (checked) next.add(item.id);
    else next.delete(item.id);
  }
  return next;
}

/** 后端生成模型 key 时对上游 id 的清洗：不合法字符换成 -，去掉开头的非字母数字 */
const sanitizeUpstreamId = (id: string) =>
  id.replace(/[^a-zA-Z0-9_.-]/g, "-").replace(/^[^a-zA-Z0-9]+/, "") || "model";

/**
 * 统计导入时 key 与其他模型冲突、被加了 `<渠道>-` 前缀的模型数，用来在提示里说明。
 * @param channelKey 渠道 key
 * @param created 导入新建的模型
 */
export function countPrefixedKeys(
  channelKey: string,
  created: ImportModelsResult["created"],
): number {
  return created.filter((item) => {
    const base = sanitizeUpstreamId(item.upstream_model);
    return item.key !== base && item.key.startsWith(`${channelKey}-${base}`);
  }).length;
}
