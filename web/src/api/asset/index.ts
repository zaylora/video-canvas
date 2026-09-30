import service from "@/utils/requests/service";
import type { AssetDto, BackendAssetDto } from "./type";

/** 后端用 0 表示「没有这个维度」（比如图片没有时长），前端统一成 null */
const positiveOrNull = (value: number | null | undefined) =>
  typeof value === "number" && value > 0 ? value : null;

/** 后端 AssetView（snake_case、数字 id）适配成前端 AssetDto；同时兼容 camelCase 响应 */
export const mapAsset = (raw: BackendAssetDto): AssetDto => ({
  id: String(raw.id),
  url: raw.url,
  kind: raw.kind,
  mimeType: raw.mime_type ?? raw.mimeType ?? "",
  byteSize: raw.byte_size ?? raw.byteSize ?? 0,
  width: positiveOrNull(raw.width),
  height: positiveOrNull(raw.height),
  durationMs: positiveOrNull(raw.duration_ms ?? raw.durationMs),
  fileName: raw.file_name ?? raw.fileName ?? null,
});

/**
 * 上传素材文件
 * @param file 待上传的文件
 * @returns 上传后的素材信息
 */
export const uploadAsset = async (file: File): Promise<AssetDto> => {
  const formData = new FormData();
  formData.append("file", file);
  return mapAsset(await service.upload<BackendAssetDto>("/assets", formData));
};
