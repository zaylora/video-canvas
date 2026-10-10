/** 前端使用的素材信息（camelCase，id 为字符串） */
export interface AssetDto {
  /** 素材 ID */
  id: string;
  /** 素材访问地址 */
  url: string;
  /** 素材类型 */
  kind: "image" | "video" | "audio";
  /** MIME 类型 */
  mimeType: string;
  /** 文件大小（字节） */
  byteSize: number;
  /** 宽度（像素），没有该维度时为 null */
  width: number | null;
  /** 高度（像素），没有该维度时为 null */
  height: number | null;
  /** 时长（毫秒），图片等没有时长时为 null */
  durationMs: number | null;
  /** 原始文件名 */
  fileName: string | null;
}

/** 后端 POST /assets 的原始响应（AssetView，snake_case，id 为数字） */
export interface BackendAssetDto {
  /** 素材 ID */
  id: number | string;
  /** 素材类型 */
  kind: "image" | "video" | "audio";
  /** 素材访问地址 */
  url: string;
  /** MIME 类型 */
  mime_type?: string | null;
  /** 文件大小（字节） */
  byte_size?: number | null;
  /** 宽度（像素），0 表示没有该维度 */
  width?: number | null;
  /** 高度（像素），0 表示没有该维度 */
  height?: number | null;
  /** 时长（毫秒），0 表示没有该维度 */
  duration_ms?: number | null;
  /** 原始文件名 */
  file_name?: string | null;
  // 兼容早先前端期望的 camelCase 形态
  /** MIME 类型（camelCase 兼容字段） */
  mimeType?: string | null;
  /** 文件大小（camelCase 兼容字段） */
  byteSize?: number | null;
  /** 时长（camelCase 兼容字段） */
  durationMs?: number | null;
  /** 原始文件名（camelCase 兼容字段） */
  fileName?: string | null;
}

/** POST /assets/upload-intents 的请求体：申请上传方式 */
export type UploadIntentBody = {
  /** 原始文件名，只用于展示 */
  file_name: string;
  /** 文件大小（字节），直传时会被签进地址或用来复核 */
  size: number;
  /** 文件的 MIME 类型，必须在后端白名单内 */
  mime_type: string;
};

/** POST /assets/upload-intents 的响应 */
export type UploadIntent = {
  /** proxy 表示走后端中转（POST /assets），direct 表示浏览器直传 */
  mode: "proxy" | "direct";
  /** 直传意图 ID，登记完成时要带上；mode=direct 时才有 */
  intent_id?: number;
  /** 直传方式：post 是表单直传（POST Policy），put 是预签名 PUT */
  method?: "post" | "put";
  /** 直传地址 */
  url?: string;
  /** post：必须原样放进表单的字段 */
  fields?: Record<string, string>;
  /** put：必须带上的请求头 */
  headers?: Record<string, string>;
  /** 凭证过期时间（ISO 时间串） */
  expires_at?: string;
};

/** 上传素材的可选项 */
export interface UploadOptions {
  /** 上传进度回调，参数是 0-100 的整数百分比；字节传完后服务端还要处理一会儿，所以到 100 不代表已完成 */
  onProgress?: (percent: number) => void;
  /** 中止信号：触发后上传被取消，uploadAsset 抛出 AbortError / CanceledError（用 isUploadAborted 认） */
  signal?: AbortSignal;
}
