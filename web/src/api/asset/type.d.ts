/** 前端使用的素材信息（camelCase，id 为字符串） */
export interface AssetDto {
  /** 素材 ID */
  id: string
  /** 素材访问地址 */
  url: string
  /** 素材类型 */
  kind: 'image' | 'video' | 'audio'
  /** MIME 类型 */
  mimeType: string
  /** 文件大小（字节） */
  byteSize: number
  /** 宽度（像素），没有该维度时为 null */
  width: number | null
  /** 高度（像素），没有该维度时为 null */
  height: number | null
  /** 时长（毫秒），图片等没有时长时为 null */
  durationMs: number | null
  /** 原始文件名 */
  fileName: string | null
}

/** 后端 POST /assets 的原始响应（AssetView，snake_case，id 为数字） */
export interface BackendAssetDto {
  /** 素材 ID */
  id: number | string
  /** 素材类型 */
  kind: 'image' | 'video' | 'audio'
  /** 素材访问地址 */
  url: string
  /** MIME 类型 */
  mime_type?: string | null
  /** 文件大小（字节） */
  byte_size?: number | null
  /** 宽度（像素），0 表示没有该维度 */
  width?: number | null
  /** 高度（像素），0 表示没有该维度 */
  height?: number | null
  /** 时长（毫秒），0 表示没有该维度 */
  duration_ms?: number | null
  /** 原始文件名 */
  file_name?: string | null
  // 兼容早先前端期望的 camelCase 形态
  /** MIME 类型（camelCase 兼容字段） */
  mimeType?: string | null
  /** 文件大小（camelCase 兼容字段） */
  byteSize?: number | null
  /** 时长（camelCase 兼容字段） */
  durationMs?: number | null
  /** 原始文件名（camelCase 兼容字段） */
  fileName?: string | null
}
