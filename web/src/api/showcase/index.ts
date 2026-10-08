import service from "@/utils/requests/service";
import type {
  BackendShowcaseDto,
  BackendShowcaseItemDto,
  BackendShowcaseSettingsDto,
  ShowcaseDto,
  ShowcaseItemDto,
  ShowcaseSettingsDto,
} from "./type";

/** 后端用 0 表示「没有该维度」，前端统一成 null */
const positiveOrNull = (value: number | null | undefined) =>
  typeof value === "number" && value > 0 ? value : null;

/**
 * 后端设置（snake_case）适配成前端 ShowcaseSettingsDto
 * @param raw 后端原始设置
 */
export const mapShowcaseSettings = (raw: BackendShowcaseSettingsDto): ShowcaseSettingsDto => ({
  clipSeconds: raw.clip_seconds,
  showOnLogin: raw.show_on_login,
  posterOnSaveData: raw.poster_only_on_save_data,
});

/**
 * 后端作品（snake_case、数字 id）适配成前端 ShowcaseItemDto
 * @param raw 后端原始作品
 */
export const mapShowcaseItem = (raw: BackendShowcaseItemDto): ShowcaseItemDto => ({
  id: String(raw.id),
  videoUrl: raw.video_url,
  posterUrl: raw.poster_url || null,
  prompt: raw.prompt,
  modelLabel: raw.model_label ?? "",
  startSec: raw.start_sec ?? 0,
  width: positiveOrNull(raw.width),
  height: positiveOrNull(raw.height),
  byteSize: positiveOrNull(raw.byte_size),
});

/**
 * 读取登录页展示（公开接口，不需要登录）。
 * 登录页没有它也能正常使用，所以失败不弹全局提示，调用方回落到默认渐变背景。
 * @returns 全局设置和启用的作品
 */
export const getShowcase = async (): Promise<ShowcaseDto> => {
  const raw = await service.get<BackendShowcaseDto>("/showcase", undefined, { silent: true });
  return {
    settings: mapShowcaseSettings(raw.settings),
    items: (raw.items ?? []).map(mapShowcaseItem),
  };
};
