import type { AssetDto } from "@/api/asset/type";

/** 弹框一次最多引入多少个素材 */
export const ASSET_PICK_LIMIT = 20;

/** 达到上限时底部栏的提示 */
export const PICK_LIMIT_HINT = `一次最多引入 ${ASSET_PICK_LIMIT} 个`;

/** 没选素材时确认按钮的提示 */
export const PICK_EMPTY_HINT = "请先选择素材";

/** 选择是不是已经满了 */
const isFull = (selected: AssetDto[], limit: number) => selected.length >= limit;

/**
 * 勾选或取消一个素材：没选过就追加到末尾，选过就移除；已满时新勾选不生效（原数组原样返回）。
 * limit 为 1 是单选：点另一个素材就换成它。
 * 选择里存整个素材而不是 ID：切换来源、类型标签后，列表换了，选中的素材仍能确认引入。
 * @param selected 当前选择，按勾选顺序
 * @param asset 被点的素材
 * @param limit 上限
 */
export function toggleAsset(
  selected: AssetDto[],
  asset: AssetDto,
  limit = ASSET_PICK_LIMIT,
): AssetDto[] {
  if (selected.some((item) => item.id === asset.id)) {
    return selected.filter((item) => item.id !== asset.id);
  }
  if (limit === 1) return [asset];
  return isFull(selected, limit) ? selected : [...selected, asset];
}

/**
 * 这张卡片现在能不能点：已选的随时能点（取消），没选的在已满后禁用；单选（limit 为 1）永远能点。
 * @param selected 当前选择
 * @param asset 卡片对应的素材
 * @param limit 上限
 */
export function canToggle(
  selected: AssetDto[],
  asset: AssetDto,
  limit = ASSET_PICK_LIMIT,
): boolean {
  return limit === 1 || selected.some((item) => item.id === asset.id) || !isFull(selected, limit);
}

/**
 * 批量追加勾选（上传完成后自动勾上新素材）：已选的不重复，超出上限的丢弃。
 * @param selected 当前选择
 * @param assets 要追加的素材
 * @param limit 上限
 * @returns 新的选择，和因超出上限没勾上的个数
 */
export function addPicked(
  selected: AssetDto[],
  assets: AssetDto[],
  limit = ASSET_PICK_LIMIT,
): { selected: AssetDto[]; dropped: number } {
  const next = [...selected];
  let dropped = 0;
  for (const asset of assets) {
    if (next.some((item) => item.id === asset.id)) continue;
    if (isFull(next, limit)) dropped++;
    else next.push(asset);
  }
  return { selected: next, dropped };
}

/** 底部栏要显示的内容 */
export type SelectionSummary = {
  /** 已选个数 */
  count: number;
  /** 「已选择 N 个素材」 */
  label: string;
  /** 确认按钮是否禁用：没选任何素材 */
  confirmDisabled: boolean;
  /** 确认按钮禁用时的提示 */
  confirmHint: string | null;
  /** 已满时的提示 */
  limitHint: string | null;
};

/** 已满时的提示：默认上限用「一次最多引入 20 个」，其它上限（补充图只剩几个名额）直接说上限 */
const limitHintOf = (limit: number) =>
  limit === ASSET_PICK_LIMIT ? PICK_LIMIT_HINT : `最多再选 ${limit} 个`;

/**
 * 底部栏的文案与按钮状态。
 * @param selected 当前选择
 * @param limit 上限
 */
export function describeSelection(
  selected: AssetDto[],
  limit = ASSET_PICK_LIMIT,
): SelectionSummary {
  const count = selected.length;
  return {
    count,
    label: `已选择 ${count} 个素材`,
    confirmDisabled: count === 0,
    confirmHint: count === 0 ? PICK_EMPTY_HINT : null,
    limitHint: limit === 1 || !isFull(selected, limit) ? null : limitHintOf(limit),
  };
}
