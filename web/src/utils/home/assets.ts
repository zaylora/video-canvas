import type { Conversation } from "@/types";

/** 资产页能筛选的内容类型：图片、视频、音频，外加还没有来源的文档 */
export type AssetType = "image" | "video" | "audio" | "doc";

/** 资产页「生成历史」里的一项（样例）：一条记录里的一个生成结果 */
export type HistoryAsset = {
  /** 唯一标识：记录 ID + 结果序号 */
  id: string;
  /** 内容类型 */
  type: Exclude<AssetType, "doc">;
  /** 占位封面的色相 */
  hue: number;
  /** 生成它的提示词 */
  prompt: string;
  /** 模型文案，取记录参数的第一项 */
  model: string;
  /** 日期标题 */
  day: string;
  /** 时长文案，图片没有 */
  duration?: string;
};

/**
 * 把对话里已完成的生成结果摊平成资产列表：生成中的不算，最新的排在前面。
 * 资产接口还没有，页面先用对话样例铺出生成历史。
 * @param conversations 对话列表
 * @returns 资产列表
 */
export const flattenHistoryAssets = (conversations: Conversation[]): HistoryAsset[] => {
  const assets: HistoryAsset[] = [];
  for (const conversation of conversations) {
    for (const record of conversation.records) {
      if (record.progress !== null) continue;
      record.results.forEach((result, index) => {
        assets.push({
          id: `${record.id}-${index}`,
          type: result.kind,
          hue: result.hue,
          prompt: record.prompt,
          model: record.meta[0] ?? "",
          day: record.day,
          duration: result.duration,
        });
      });
    }
  }
  return assets.reverse();
};

/**
 * 按类型和关键字筛资产
 * @param assets 资产列表
 * @param type 内容类型；doc 目前没有来源，永远为空
 * @param keyword 提示词里要包含的关键字，空串不筛
 * @returns 筛过的列表，保持原顺序
 */
export const filterHistoryAssets = (assets: HistoryAsset[], type: AssetType, keyword: string) => {
  const word = keyword.trim();
  return assets.filter((item) => item.type === type && (!word || item.prompt.includes(word)));
};
