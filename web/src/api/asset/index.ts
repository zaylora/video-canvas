import { toast } from "sonner";

import { buildIntentBody, planUpload, sendDirect } from "@/utils/asset/direct-upload";
import service from "@/utils/requests/service";
import type { AssetDto, BackendAssetDto, UploadIntent, UploadOptions } from "./type";

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

/** 直传失败降级到中转时的提示；固定 id，同一时间只会出现一条 */
const FALLBACK_TOAST_ID = "asset-direct-fallback";

/**
 * 后端中转上传：文件交给后端，由后端写入默认存储
 * @param file 待上传的文件
 * @param options 进度回调与中止信号
 */
const uploadViaProxy = async (file: File, options: UploadOptions): Promise<AssetDto> => {
  const { onProgress, signal } = options;
  const formData = new FormData();
  formData.append("file", file);
  return mapAsset(
    await service.upload<BackendAssetDto>("/assets", formData, {
      signal,
      onUploadProgress: (event) => {
        if (onProgress && event.total) onProgress(Math.floor((event.loaded / event.total) * 100));
      },
    }),
  );
};

/**
 * 申请上传方式。申请失败不算上传失败（后端旧版本没有这个接口、存储暂不可用等），
 * 返回 null 让调用方走中转；因为失败后还有退路，所以不弹全局错误提示。
 * @param file 待上传的文件
 * @param signal 中止信号
 */
const requestUploadIntent = async (
  file: File,
  signal?: AbortSignal,
): Promise<UploadIntent | null> => {
  const body = buildIntentBody(file);
  if (!body) return null;
  try {
    return await service.post<UploadIntent>("/assets/upload-intents", body, {
      silent: true,
      signal,
    });
  } catch {
    return null;
  }
};

/**
 * 上传素材文件。
 * 先申请上传方式：默认存储开启了浏览器直传就直传到对象存储，再登记成素材；
 * 其余情况（没开直传、申请失败、文件类型为空）走后端中转。直传失败（CORS 未配置、断网等）
 * 会自动降级到中转并提示一次，调用方无感。登记失败（类型不合法、超限）是真错误，不降级。
 * 带了中止信号时，取消会让它抛出 AbortError / CanceledError，取消不算失败、不降级；
 * 降级到中转后进度会从 0 重新开始。
 * @param file 待上传的文件
 * @param options 进度回调与中止信号，都可不传
 * @returns 上传后的素材信息
 */
export const uploadAsset = async (file: File, options: UploadOptions = {}): Promise<AssetDto> => {
  const { signal } = options;
  const plan = planUpload(await requestUploadIntent(file, signal), file);
  if (plan.kind === "proxy") return uploadViaProxy(file, options);

  if (!(await sendDirect(plan.request, options))) {
    toast.info("直传失败，已改用服务器中转", { id: FALLBACK_TOAST_ID });
    return uploadViaProxy(file, options);
  }
  return mapAsset(
    await service.post<BackendAssetDto>(
      `/assets/upload-intents/${plan.intentId}/complete`,
      undefined,
      {
        signal,
      },
    ),
  );
};

/**
 * 获取素材信息（地址、尺寸、文件名）。「重新编辑」还原参考图缩略图时用，素材已被清理等失败不弹提示，
 * 调用方自己决定怎么显示。
 * @param id 素材 ID
 * @returns 素材信息
 */
export const getAsset = async (id: string | number): Promise<AssetDto> =>
  mapAsset(await service.get<BackendAssetDto>(`/assets/${id}`, undefined, { silent: true }));
