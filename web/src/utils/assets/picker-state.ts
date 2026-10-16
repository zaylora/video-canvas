import type { AssetDto } from "@/api/asset/type";
import type { AssetKind } from "@/api/asset-library/type";

import { ASSET_PICK_LIMIT, addPicked } from "./picker-selection";

/** 弹框的来源：个人资产库、生成历史 */
export type PickerSource = "library" | "history";

/** 弹框当前看的位置：来源、类型标签，个人资产库里还有所在文件夹 */
export type PickerView = {
  /** 来源 */
  source: PickerSource;
  /** 类型标签 */
  kind: AssetKind;
  /** 个人资产库的当前文件夹，null 是根目录；生成历史恒为 null */
  folder: string | null;
};

/** 每次打开弹框的起点：个人资产库根目录的图片 */
export const INITIAL_PICKER_VIEW: PickerView = { source: "library", kind: "image", folder: null };

/** 切来源：类型标签沿用，文件夹回到根目录（两个来源的文件夹概念不通） */
export const switchSource = (view: PickerView, source: PickerSource): PickerView => ({
  ...view,
  source,
  folder: null,
});

/** 切类型标签 */
export const switchKind = (view: PickerView, kind: AssetKind): PickerView => ({ ...view, kind });

/** 进入文件夹；只有个人资产库有文件夹，生成历史里原样返回 */
export const enterFolder = (view: PickerView, folder: string): PickerView =>
  view.source === "library" ? { ...view, folder } : view;

/** 回到根目录 */
export const leaveFolder = (view: PickerView): PickerView => ({ ...view, folder: null });

/**
 * 弹框内上传的文件归入哪个文件夹：在个人资产库里就是当前所在文件夹（根目录为 null）。
 * @param view 当前位置
 */
export const uploadFolder = (view: PickerView): string | null =>
  view.source === "library" ? view.folder : null;

/** 一个文件上传完成后弹框的新状态 */
export type UploadDoneResult = {
  /** 新的位置：类型标签切到刚上传的素材类型，让它立刻出现在眼前 */
  view: PickerView;
  /** 新的选择：上传的素材自动勾上 */
  selected: AssetDto[];
  /** 因为已选满上限没勾上的个数 */
  dropped: number;
};

/**
 * 弹框内一个文件上传完成：自动勾选（受上限约束），并把类型标签切到该素材的类型。
 * @param view 当前位置
 * @param selected 当前选择
 * @param asset 刚上传并登记好的素材
 * @param limit 上限
 */
export function applyUploadDone(
  view: PickerView,
  selected: AssetDto[],
  asset: AssetDto,
  limit = ASSET_PICK_LIMIT,
): UploadDoneResult {
  const picked = addPicked(selected, [asset], limit);
  return { view: switchKind(view, asset.kind), ...picked };
}
