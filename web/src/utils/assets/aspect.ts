import type { AssetDto } from "@/api/asset/type";

/** 最细长的竖图和最扁的横图：超出就收到这里，免得一张卡占满一列或细成一条 */
const TALLEST = 9 / 16;
const WIDEST = 21 / 9;

/**
 * 瀑布流卡片的画幅（CSS aspect-ratio 的值）：有宽高按素材真实比例，超出上下限收到 9:16 / 21:9；
 * 缺宽高时视频 16:10、图片正方形；音频没有画面，固定 16:10 的波形卡。
 * @param asset 素材的类型与宽高
 */
export function assetAspect(asset: Pick<AssetDto, "kind" | "width" | "height">) {
  if (asset.kind === "audio") return "16 / 10";
  const { width, height } = asset;
  if (!width || !height) return asset.kind === "video" ? "16 / 10" : "1 / 1";
  const ratio = width / height;
  if (ratio < TALLEST) return "9 / 16";
  if (ratio > WIDEST) return "21 / 9";
  return `${width} / ${height}`;
}
